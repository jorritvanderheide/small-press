# A stand-in for a site's flake, for the module's VM test (../test.nix):
# no inputs, so the VM builds it without the network, with busybox as the
# only tool. @system@ and @busybox@ are filled in by the test.
{
  outputs = _: {
    packages.@system@ = rec {
      program = derivation {
        name = "stand-in";
        system = "@system@";
        builder = "@busybox@/bin/sh";
        args = [ ./program.sh ];
        PATH = "@busybox@/bin";
      };

      site = derivation {
        name = "site";
        system = "@system@";
        builder = "@busybox@/bin/sh";
        args = [ ./site.sh ];
        PATH = "@busybox@/bin";
        page = ./page.txt;
        inherit program;
      };
    };
  };
}
