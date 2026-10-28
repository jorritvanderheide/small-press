{
  lib,
  runCommand,
  program,
  # The commit count, the build number at the foot of every page
  # (docs/architecture.md, Time). From the flake, not a counter file, so the
  # same commit gives the same site.
  build,
}:
let
  # example/content/ only, so the site is built again when it changes, and
  # the program when the code does.
  src = lib.fileset.toSource {
    root = ../example;
    fileset = ../example/content;
  };
in
runCommand "site" { nativeBuildInputs = [ program ]; } ''
  ${lib.getExe program} -content ${src}/content -out $out -build ${toString build}
''
