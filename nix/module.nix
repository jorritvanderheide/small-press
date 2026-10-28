{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.small-press;

  # Every response's headers. nginx drops the server's add_header in a
  # location with an add_header of its own, so such a location repeats them.
  headers = ''
    add_header Content-Security-Policy "default-src 'none'; img-src 'self'; style-src 'self'; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "DENY" always;
    add_header Referrer-Policy "no-referrer" always;
  '';

  # The same hardening for both services: files, the network, and nothing
  # else.
  hardening = {
    CapabilityBoundingSet = "";
    LockPersonality = true;
    NoNewPrivileges = true;
    PrivateDevices = true;
    PrivateTmp = true;
    ProtectClock = true;
    ProtectControlGroups = true;
    ProtectHome = true;
    ProtectHostname = true;
    ProtectKernelLogs = true;
    ProtectKernelModules = true;
    ProtectKernelTunables = true;
    ProtectProc = "invisible";
    ProtectSystem = "strict";
    RestrictAddressFamilies = [
      "AF_UNIX"
      "AF_INET"
      "AF_INET6"
    ];
    RestrictNamespaces = true;
    RestrictRealtime = true;
    RestrictSUIDSGID = true;
    SystemCallArchitectures = "native";
    SystemCallFilter = [
      "@system-service"
      "~@privileged"
    ];
  };

  siteOptions =
    { name, ... }:
    {
      options = {
        domain = lib.mkOption {
          type = lib.types.str;
          example = "bw20.nl";
          description = ''
            Serve the site with nginx on this domain. TLS is yours to add to
            {option}`services.nginx.virtualHosts.<domain>`, e.g. `enableACME`
            and `forceSSL`.
          '';
        };

        repository = lib.mkOption {
          type = lib.types.str;
          example = "https://codeberg.org/BW20/personal-blog";
          description = ''
            The site's repository, over HTTPS, without credentials: a flake
            whose `site` is the site and whose `program` is its program. The
            deploy asks it for the newest commit, and builds that.
          '';
        };

        program = lib.mkOption {
          type = lib.types.str;
          default = name;
          example = "personal-blog";
          description = "The name of the program in the flake's `program`, as in its `bin/`.";
        };

        branch = lib.mkOption {
          type = lib.types.str;
          default = "main";
          description = "The branch that is published.";
        };

        deploy = {
          interval = lib.mkOption {
            type = lib.types.str;
            default = "5min";
            description = ''
              How long after one look at the repository the deploy looks again.
              A push from elsewhere, such as a laptop, is live within this and a
              build. A post the receiver takes starts the deploy at once.
            '';
          };

          onFailure = lib.mkOption {
            type = lib.types.listOf lib.types.str;
            default = [ ];
            example = [ "notify-failure@%n.service" ];
            description = ''
              Units to start when a commit doesn't build, such as one that sends
              a notification. Each commit fails once: the deploy doesn't try it
              again, so this isn't started every minute. The site that was
              there stays.
            '';
          };

          onSuccess = lib.mkOption {
            type = lib.types.listOf lib.types.str;
            default = [ ];
            example = [ "report-deploy@success.service" ];
            description = ''
              Units to start when a deploy has published a new commit, such as
              one that tells a status page. Not when there was nothing new.
            '';
          };
        };

        receiver = {
          enable = lib.mkEnableOption "the receiver, which takes posts over Micropub and commits them";

          tokenFile = lib.mkOption {
            type = lib.types.nullOr lib.types.str;
            default = null;
            example = "/run/secrets/blog-receiver-token";
            description = ''
              A file with the token the plugin sends, a long random string.
              systemd hands it to the receiver (`LoadCredential`), so it stays
              out of the Nix store and needn't be readable by the receiver's
              user.
            '';
          };

          pushUrl = lib.mkOption {
            type = lib.types.nullOr lib.types.str;
            default = null;
            example = "ssh://git@codeberg.org/BW20/personal-blog.git";
            description = "Where the receiver pushes its commits, over SSH.";
          };

          sshKeyFile = lib.mkOption {
            type = lib.types.nullOr lib.types.str;
            default = null;
            example = "/run/secrets/blog-receiver-ssh-key";
            description = ''
              The private key the receiver pushes with: a deploy key with write
              access to this repository only. Handed over as the token is.
            '';
          };

          knownHosts = lib.mkOption {
            type = lib.types.lines;
            default = "";
            example = "codeberg.org ssh-ed25519 AAAA...";
            description = "The forge's SSH host keys, as `known_hosts` lines. The receiver trusts no others.";
          };

          author = lib.mkOption {
            type = lib.types.nullOr lib.types.str;
            default = null;
            example = "In the Open <receiver@bw20.nl>";
            description = "The author of the receiver's commits, as Name <address>.";
          };

          socket = lib.mkOption {
            type = lib.types.str;
            default = "/run/small-press-${name}-receiver/receiver.sock";
            readOnly = true;
            description = "The Unix socket the receiver listens on; it has no TCP port.";
          };
        };
      };
    };

  # Everything one site is: its users, its deploy, its receiver and its
  # server, each named for the site, so no two sites share a file or a key:
  # the base, and the receiver when it is on.
  siteConfig =
    name: s:
    let
      unit = "small-press-${name}";
      user = unit;
      receiverUser = "${unit}-receiver";

      # What the deploy builds, as links nginx and the receiver follow. Each
      # moves in one step, when its build is done (docs/architecture.md,
      # Deployment).
      stateDir = "/var/lib/${unit}";
      receiverDir = "/var/lib/${receiverUser}";
      site = "${stateDir}/site";
      program = "${stateDir}/program/bin/${s.program}";

      # The 403 for the crawlers robots.txt names, as the build writes it, for
      # every location but robots.txt itself. A glob, so nginx starts before
      # the first deploy, when there is no site yet.
      crawlers = ''
        include ${site}/nginx/location/*.conf;
      '';

      # Run as root after a deploy that built something: nginx reads the
      # build's includes again, the units in deploy.onSuccess start, and the
      # receiver runs the program just built, so it checks posts by the same
      # rules as the build. The socket is started again in case it gave up
      # before the first deploy, when there was no program to start.
      apply = pkgs.writeShellScript "${unit}-apply" (
        ''
          [ -e ${stateDir}/apply ] || exit 0
          rm ${stateDir}/apply
          systemctl=${config.systemd.package}/bin/systemctl
          $systemctl reload nginx.service
        ''
        + lib.optionalString (s.deploy.onSuccess != [ ]) ''
          $systemctl start --no-block ${lib.escapeShellArgs s.deploy.onSuccess}
        ''
        + lib.optionalString s.receiver.enable ''
          $systemctl reset-failed ${receiverUser}.socket ${receiverUser}.service || true
          $systemctl start ${receiverUser}.socket
          $systemctl try-restart ${receiverUser}.service
        ''
      );
    in
    {
      base = {
        # The deploy runs nix build as its own user, which the daemon has to
        # let in. The module can't add it itself: on a host that leaves the
        # list at its default, "*", a value here would replace that, and shut
        # everyone else out.
        assertions = [
          {
            assertion =
              let
                allowed = config.nix.settings.allowed-users;
              in
              builtins.elem "*" allowed || builtins.elem user allowed || builtins.elem "@${user}" allowed;
            message = "services.small-press.sites.${name}: the deploy runs nix build as the user ${user}. Add it to nix.settings.allowed-users.";
          }
        ];

        users.users.${user} = {
          isSystemUser = true;
          group = user;
          home = stateDir;
        };
        users.groups.${user} = { };

        # Builds the newest commit, and moves the links when both builds are
        # done: the site first, which the program is part of, so a commit
        # that doesn't build changes nothing.
        systemd.services."${unit}-deploy" = {
          description = "Build and publish the newest commit of ${s.domain}";
          wants = [ "network-online.target" ];
          after = [ "network-online.target" ];
          unitConfig.OnFailure = s.deploy.onFailure;
          path = [
            config.nix.package
            pkgs.git
          ];
          environment = {
            REPOSITORY = s.repository;
            BRANCH = s.branch;
          };

          serviceConfig = hardening // {
            Type = "oneshot";
            User = user;
            Group = user;
            StateDirectory = unit;
            StateDirectoryMode = "0755"; # nginx follows the site link
            WorkingDirectory = stateDir;
            ExecStartPost = "+${apply}";
          };

          script = ''
            # A forge that doesn't answer is tried again at the next look,
            # without a failure for every minute it's away.
            if ! line=$(git ls-remote --exit-code "$REPOSITORY" "refs/heads/$BRANCH"); then
              echo "Couldn't ask $REPOSITORY for $BRANCH; trying again later."
              exit 0
            fi
            rev=''${line%%[[:space:]]*}

            [ "$rev" = "$(cat deployed 2>/dev/null)" ] && exit 0
            if [ "$rev" = "$(cat failed 2>/dev/null)" ]; then
              exit 0
            fi

            flake="git+$REPOSITORY?ref=refs/heads/$BRANCH&rev=$rev"
            if ! nix build "$flake#site" --out-link site || ! nix build "$flake#program" --out-link program; then
              echo "$rev" > failed
              echo "$rev doesn't build; the site that was there stays."
              exit 1
            fi

            echo "$rev" > deployed
            touch apply
            echo "Published $rev."
          '';
        };

        systemd.timers."${unit}-deploy" = {
          wantedBy = [ "timers.target" ];
          timerConfig = {
            OnBootSec = "1min";
            OnUnitInactiveSec = s.deploy.interval;
          };
        };

        # Files in the Nix store all have the same mtime, but nixpkgs' nginx
        # makes ETags from the store path instead, so caching just works.
        services.nginx.virtualHosts.${s.domain} = {
          root = site;

          # No access log: a site has no use for its readers' addresses. The
          # build's server includes are the redirects of old slugs.
          extraConfig = ''
            access_log off;
            error_page 404 /404.html;
            ${headers}
            include ${site}/nginx/server/*.conf;
          '';

          locations = {
            # HTML is asked for again each time, and its ETag answers with a
            # 304 when nothing changed: a new post shows at once.
            "/".extraConfig = ''
              expires -1;
              ${crawlers}
            '';

            # Hashed (?v=), so a year is safe: a new file has a new address.
            "/static/".extraConfig = ''
              expires 1y;
              ${crawlers}
            '';

            # Everyone may read it, the crawlers it names included.
            "= /robots.txt" = { };

            # The build's includes are for nginx, not for readers.
            "^~ /nginx/".return = "404";
          };
        };
      };

      receiver = {
        assertions =
          map
            (option: {
              assertion = s.receiver.${option} != null && s.receiver.${option} != "";
              message = "services.small-press.sites.${name}.receiver.${option} is needed for the receiver.";
            })
            [
              "tokenFile"
              "pushUrl"
              "sshKeyFile"
              "knownHosts"
              "author"
            ];

        users.users.${receiverUser} = {
          isSystemUser = true;
          group = receiverUser;
          home = receiverDir;
        };
        users.groups.${receiverUser} = { };

        # A post the receiver pushed is built at once, not at the timer's next
        # look: it writes the time to `pushed` after each push, and a change to
        # that file starts the deploy. A deploy already running takes the start
        # as its own, and a commit that came in during it waits for the timer.
        systemd.paths."${unit}-pushed" = {
          description = "Start the deploy of ${s.domain} when the receiver has pushed";
          wantedBy = [ "paths.target" ];
          pathConfig = {
            PathChanged = "${receiverDir}/pushed";
            Unit = "${unit}-deploy.service";
          };
        };

        # systemd owns the socket: only the proxy's group may open it, and the
        # proxy keeps its connection while the receiver restarts after a
        # deploy.
        systemd.sockets.${receiverUser} = {
          description = "Receiver socket of ${s.domain}";
          wantedBy = [ "sockets.target" ];
          listenStreams = [ s.receiver.socket ];
          socketConfig = {
            SocketUser = receiverUser;
            SocketGroup = cfg.proxyGroup;
            SocketMode = "0660";
          };
        };

        # The program the deploy built, not one from this configuration, so
        # the receiver and the build can't disagree about what breaks the site.
        systemd.services.${receiverUser} = {
          description = "Receiver of ${s.domain}, which takes posts over Micropub";
          requires = [ "${receiverUser}.socket" ];
          wants = [ "network-online.target" ];
          after = [
            "network-online.target"
            "${receiverUser}.socket"
          ];
          path = [
            pkgs.git
            pkgs.openssh
          ];

          serviceConfig = hardening // {
            ExecStart = lib.escapeShellArgs [
              program
              "-receive"
              "-listen"
              "systemd"
              "-base-url"
              "https://${s.domain}"
              "-push-url"
              s.receiver.pushUrl
              "-branch"
              s.branch
              "-author"
              s.receiver.author
              "-token-file"
              "%d/token"
              "-ssh-key-file"
              "%d/ssh-key"
              "-known-hosts"
              (pkgs.writeText "${receiverUser}-known-hosts" s.receiver.knownHosts)
              "-state"
              receiverDir
            ];
            LoadCredential = [
              "token:${s.receiver.tokenFile}"
              "ssh-key:${s.receiver.sshKeyFile}"
            ];
            User = receiverUser;
            Group = receiverUser;
            StateDirectory = receiverUser;
            StateDirectoryMode = "0700";
            UMask = "0077";
            Restart = "on-failure";
            RestartPreventExitStatus = 2;
            RestartSec = "5s";
            MemoryDenyWriteExecute = true;
          };
        };

        services.nginx.virtualHosts.${s.domain}.locations = {
          # Where the plugin finds the receiver. try_files serves the front
          # page here: index would hand it to "/", without the Link header.
          "= /".extraConfig = ''
            try_files /index.html =404;
            expires -1;
            ${headers}
            add_header Link '</micropub>; rel="micropub"' always;
            ${crawlers}
          '';

          # The post and its images, the largest 20 MB.
          "/micropub" = {
            proxyPass = "http://unix:${s.receiver.socket}";
            recommendedProxySettings = true;
            extraConfig = ''
              client_max_body_size 21m;
            '';
          };
        };
      };
    };
in
{
  options.services.small-press = {
    sites = lib.mkOption {
      type = lib.types.attrsOf (lib.types.submodule siteOptions);
      default = { };
      example = lib.literalExpression ''
        {
          bw20 = {
            domain = "bw20.nl";
            repository = "https://codeberg.org/BW20/personal-blog";
            program = "personal-blog";
          };
        }
      '';
      description = ''
        The sites to serve, each from its own repository, by a name for its
        users, units and folders: `small-press-<name>-deploy` and the rest.
      '';
    };

    proxyGroup = lib.mkOption {
      type = lib.types.str;
      default = config.services.nginx.group;
      defaultText = lib.literalExpression "config.services.nginx.group";
      description = "Group of the reverse proxy: only it (and a site's receiver) may open the receiver's socket.";
    };
  };

  # Each option is merged from every site's fragments, rather than the
  # fragments merged whole: which options the module sets can't depend on
  # the sites, or the module system loops looking for them.
  config =
    let
      fragments = lib.concatLists (
        lib.mapAttrsToList (
          name: s:
          let
            f = siteConfig name s;
          in
          [ f.base ] ++ lib.optional s.receiver.enable f.receiver
        ) cfg.sites
      );
      merged = path: lib.mkMerge (map (lib.attrByPath path { }) fragments);
    in
    lib.mkIf (cfg.sites != { }) {
      assertions = lib.concatMap (f: f.assertions or [ ]) fragments;
      users.users = merged [
        "users"
        "users"
      ];
      users.groups = merged [
        "users"
        "groups"
      ];
      systemd.services = merged [
        "systemd"
        "services"
      ];
      systemd.timers = merged [
        "systemd"
        "timers"
      ];
      systemd.paths = merged [
        "systemd"
        "paths"
      ];
      systemd.sockets = merged [
        "systemd"
        "sockets"
      ];
      services.nginx.enable = true;
      services.nginx.virtualHosts = merged [
        "services"
        "nginx"
        "virtualHosts"
      ];
    };
}
