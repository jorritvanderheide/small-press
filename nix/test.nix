# The module in VMs, without the network.
#
# deploy: two sites' deploys, each from its own repository on the VM.
# Building a real site's flake there would need nixpkgs and Go without the
# network, so the sites in it are a stand-in (test/) that writes what the
# real build does: a receiver that waits for the first deploy, a first
# deploy, one with nothing new, a commit that doesn't build, one that does
# again, and a second site beside the first.
#
# site: the example site, built here, as nginx serves it to a reader, and its
# real receiver, which commits a post it takes to a repository on the VM.
{
  pkgs,
  module,
  site,
  program,
  content,
}:
let
  inherit (pkgs.stdenv.hostPlatform) system;

  standIn = pkgs.runCommand "small-press-stand-in" { } ''
    mkdir $out
    cp ${./test/program.sh} $out/program.sh
    cp ${./test/site.sh} $out/site.sh
    substitute ${./test/flake.nix} $out/flake.nix \
      --replace-fail @system@ ${system} \
      --replace-fail @busybox@ ${pkgs.busybox}
  '';
in
pkgs.testers.runNixOSTest {
  name = "small-press";

  nodes.deploy = {
    imports = [ module ];

    # The stand-in's builder is busybox by its path, not a dependency, so
    # builds run outside the sandbox. Nothing to substitute without the
    # network.
    nix.settings = {
      experimental-features = [
        "nix-command"
        "flakes"
      ];
      sandbox = false;
      substituters = pkgs.lib.mkForce [ ];
    };
    virtualisation.additionalPaths = [
      pkgs.busybox
      standIn
    ];
    environment.systemPackages = [ pkgs.git ];

    systemd.services.deploy-failed = {
      serviceConfig.Type = "oneshot";
      script = "touch /tmp/deploy-failed";
    };

    systemd.services.deploy-published = {
      serviceConfig.Type = "oneshot";
      script = "touch /tmp/deploy-published";
    };

    services.small-press.sites = {
      blog = {
        domain = "blog.test";
        repository = "file:///srv/blog.git";
        program = "stand-in";
        deploy.onFailure = [ "deploy-failed.service" ];
        deploy.onSuccess = [ "deploy-published.service" ];

        receiver = {
          enable = true;
          tokenFile = "${pkgs.writeText "token" "secret"}";
          pushUrl = "ssh://git@forge.test/blog.git";
          sshKeyFile = "${pkgs.writeText "ssh-key" "not a key"}";
          knownHosts = "forge.test ssh-ed25519 AAAAtest";
          author = "Receiver <receiver@blog.test>";
        };
      };

      # A second site beside it, from its own repository, without a receiver.
      other = {
        domain = "other.test";
        repository = "file:///srv/other.git";
        program = "stand-in";
      };
    };

  };

  nodes.site = { lib, ... }: {
    imports = [ module ];

    # The timer's first look an hour away, so a deploy the test sees is one
    # the receiver started.
    systemd.timers.small-press-example-deploy.timerConfig.OnBootSec = lib.mkForce "1h";

    # Where the deploy would have put them. The deploy finds no repository
    # here, and leaves them.
    systemd.tmpfiles.rules = [
      "d /var/lib/small-press-example 0755 small-press-example small-press-example -"
      "L+ /var/lib/small-press-example/site - - - - ${site}"
      "L+ /var/lib/small-press-example/program - - - - ${program}"
    ];
    environment.systemPackages = [ pkgs.git ];

    services.small-press.sites.example = {
      domain = "example.test";
      repository = "file:///srv/none";

      receiver = {
        enable = true;
        tokenFile = "${pkgs.writeText "token" "secret"}";
        # The sandbox lets it write only to its state directory, so the
        # repository it pushes to is there too: on a host, it pushes over SSH.
        pushUrl = "file:///var/lib/small-press-example-receiver/remote.git";
        sshKeyFile = "${pkgs.writeText "ssh-key" "not a key"}";
        knownHosts = "forge.test ssh-ed25519 AAAAtest";
        author = "Receiver <receiver@example.test>";
      };
    };
  };

  testScript = ''
    def get(machine, path, *args, host="blog.test"):
        return machine.succeed(f"curl -s -H 'Host: {host}' {' '.join(args)} http://localhost{path}")

    def status(machine, path, *args, host="blog.test"):
        return get(machine, path, "-o /dev/null", "-w '%{http_code}'", *args, host=host)

    start_all()

    # The deploy.

    def commit(page, name="blog"):
        deploy.succeed(
            f"echo '{page}' > /tmp/{name}/page.txt",
            f"git -C /tmp/{name} add -A",
            f"git -C /tmp/{name} -c user.name=t -c user.email=t@t commit -qm '{page}'",
            # Nix reads only a repository its user owns, and git pushes only
            # to one root owns: it changes hands around the push.
            f"chown -R root:root /srv/{name}.git",
            f"git -C /tmp/{name} push -q /srv/{name}.git HEAD:refs/heads/main",
            f"chown -R small-press-{name}:small-press-{name} /srv/{name}.git",
        )

    deploy.wait_for_unit("nginx.service")

    with subtest("nginx starts before the first deploy, and the receiver waits for it"):
        # Neither the socket nor the receiver starts without a program, so a
        # request before the first deploy fails no unit, and with it no
        # switch of the host.
        get(deploy, "/micropub")
        for unit in ["small-press-blog-receiver.socket", "small-press-blog-receiver.service"]:
            deploy.fail(f"systemctl is-active {unit}")
            deploy.fail(f"systemctl is-failed {unit}")

    for name in ["blog", "other"]:
        deploy.succeed(
            f"git init -q --bare -b main /srv/{name}.git",
            f"git init -q -b main /tmp/{name}",
            f"cp ${standIn}/* /tmp/{name}/",
        )

    with subtest("a deploy publishes the newest commit"):
        commit("first")
        deploy.succeed("systemctl start small-press-blog-deploy.service")
        assert "first" in get(deploy, "/")
        deploy.wait_for_file("/tmp/deploy-published")

    with subtest("the receiver's link, and its socket once there is a program"):
        deploy.succeed("systemctl is-active small-press-blog-receiver.socket")
        assert 'rel="micropub"' in get(deploy, "/", "-D -", "-o /dev/null")

    with subtest("the build's includes"):
        # nginx reloads after a deploy, and its new workers start a moment
        # later: the redirect shows once they have.
        deploy.wait_until_succeeds(
            "curl -s -H 'Host: blog.test' -o /dev/null -w '%{http_code}' http://localhost/log/old/ | grep -qx 301",
            timeout=30,
        )
        for want, args in [("403", ["/log/hello/", "-A GPTBot"]), ("404", ["/nginx/server/aliases.conf"])]:
            got = status(deploy, *args)
            assert want in got, f"{args}: {got}, want {want}"

    with subtest("a deploy with nothing new does nothing"):
        deploy.succeed("rm /tmp/deploy-published")
        deploy.succeed("systemctl start small-press-blog-deploy.service")
        assert "first" in get(deploy, "/")
        deploy.fail("test -e /tmp/deploy-published")

    with subtest("a commit that doesn't build changes nothing, and fails once"):
        commit("fail")
        deploy.fail("systemctl start small-press-blog-deploy.service")
        deploy.wait_for_file("/tmp/deploy-failed")
        assert "first" in get(deploy, "/")
        deploy.succeed("rm /tmp/deploy-failed")
        deploy.succeed("systemctl start small-press-blog-deploy.service")
        deploy.fail("test -e /tmp/deploy-failed")

    with subtest("the next commit that builds publishes again"):
        commit("second")
        deploy.succeed("systemctl start small-press-blog-deploy.service")
        assert "second" in get(deploy, "/")
        deploy.succeed("systemctl is-active small-press-blog-receiver.socket")

    with subtest("a second site deploys from its own repository, as its own user"):
        commit("other", name="other")
        deploy.succeed("systemctl start small-press-other-deploy.service")
        assert "other" in get(deploy, "/", host="other.test")
        assert "second" in get(deploy, "/")
        assert "micropub" not in get(deploy, "/", "-D -", "-o /dev/null", host="other.test")
        owner = deploy.succeed("stat -c %U /var/lib/small-press-other/deployed").strip()
        assert owner == "small-press-other", owner

    # The example site.

    site.wait_for_unit("nginx.service")

    def page(path, *args):
        return get(site, path, *args, host="example.test")

    with subtest("the example site's pages"):
        home = page("/")
        assert "A site on small-press" in home, home
        about = page("/about/")
        assert "<h1>About</h1>" in about, about
        assert "<h1>Log</h1>" in page("/log/")
        assert "<feed" in page("/feed.xml")

    with subtest("the example site's headers"):
        headers = page("/about/", "-D -", "-o /dev/null")
        assert "default-src 'none'; script-src 'self';" in headers, headers
        assert "X-Content-Type-Options: nosniff" in headers, headers
        assert "Cache-Control: no-cache" in headers, headers
        assert "micropub" not in headers, headers
        css = site.succeed("grep -o '/static/site.css?v=[0-9a-f]*' /var/lib/small-press-example/site/index.html").strip()
        assert "max-age=31536000" in page(css, "-D -", "-o /dev/null")

    with subtest("the example site's crawlers, 404 and includes"):
        assert "403" in status(site, "/about/", "-A GPTBot", host="example.test")
        assert "200" in status(site, "/robots.txt", "-A GPTBot", host="example.test")
        assert "404" in status(site, "/nope/", host="example.test")
        assert "Not here" in page("/nope/")
        assert "404" in status(site, "/nginx/location/crawlers.conf", host="example.test")
        assert "301" in status(site, "/log/old/", host="example.test")

    # The real receiver.

    remote = "/var/lib/small-press-example-receiver/remote.git"
    site.succeed(
        "mkdir -p /var/lib/small-press-example-receiver",
        f"git init -q --bare -b main {remote}",
        "git init -q -b main /tmp/work",
        "cp -r ${content} /tmp/work/content",
        "chmod -R u+w /tmp/work",
        "git -C /tmp/work add -A",
        "git -C /tmp/work -c user.name=t -c user.email=t@t commit -qm content",
        f"git -C /tmp/work push -q {remote} HEAD:refs/heads/main",
        "chown -R small-press-example-receiver:small-press-example-receiver /var/lib/small-press-example-receiver",
    )

    def micropub(body, *args):
        site.succeed(f"cat > /tmp/post.json <<'EOF'\n{body}\nEOF")
        return page("/micropub", "-X POST", "-H 'Content-Type: application/json'", "--data-binary @/tmp/post.json", *args)

    def show(what):
        return site.succeed(f"git -c safe.directory='*' --git-dir {remote} {what}")

    token = "-H 'Authorization: Bearer secret'"
    post = '{"type": ["h-entry"], "properties": {"name": ["From the VM"], "summary": ["A test."], "content": ["Said in a VM, as [(Marsh 2020)](#ref-marsh2020) has it."], "category": ["tech"], "mp-slug": ["from-the-vm"], "references": [{"key": "marsh2020", "text": "Marsh, Ann. 2020. *Archives*.", "url": "https://doi.org/10.1/x"}]}}'

    with subtest("the receiver wants its token"):
        out = micropub(post, "-w ' %{http_code}'")
        assert out.endswith("401"), out

    with subtest("the receiver refuses a post that would break the build"):
        broken = post.replace('"summary": ["A test."], ', "")
        out = micropub(broken, token, "-w ' %{http_code}'")
        assert out.endswith("400") and "missing summary" in out, out

    with subtest("the receiver refuses a post the site's pages can't take"):
        # The example's post page has a section with the id references.
        clash = post.replace("has it.", "has it.\\n\\n## References")
        out = micropub(clash, token, "-w ' %{http_code}'")
        assert out.endswith("400") and "own section" in out, out

    deploys = lambda: site.succeed("systemctl show -p InvocationID --value small-press-example-deploy.service").strip()
    before = deploys()

    with subtest("the receiver commits a post"):
        out = micropub(post, token, "-D -", "-o /dev/null")
        assert "202" in out and "Location: https://example.test/log/from-the-vm/" in out, out
        assert show("log -1 --format=%s") == "publish: from-the-vm\n"
        text = show("show main:content/log/from-the-vm/index.md")
        assert 'generator = "micropub"' in text and "[(Marsh 2020)](#ref-marsh2020)" in text, text
        work = show("show main:content/works/marsh2020.toml")
        assert "Marsh, Ann. 2020. *Archives*." in work, work

    with subtest("a post the receiver pushed starts the deploy at once"):
        site.wait_until_succeeds(
            f"test \"$(systemctl show -p InvocationID --value small-press-example-deploy.service)\" != '{before}'",
            timeout=30,
        )
  '';
}
