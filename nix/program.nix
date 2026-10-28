{
  lib,
  buildGoModule,
  git,
}:
let
  root = ../.;
in
buildGoModule {
  pname = "example";
  version = "dev";

  # The library and the example site's program: everything but its
  # content/, so a commit that only changes the content finds it in the
  # store already. The templates under example/web/ are embedded in it.
  src = lib.fileset.toSource {
    inherit root;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      (lib.fileset.fileFilter (f: f.hasExt "go") root)
      ../build/crawlers.txt
      ../build/meta.html
      ../example/web
    ];
  };

  vendorHash = "sha256-pRmtJsS55ntxO0Hs3Ig2EPTB/pnYN5dVkUZ7MW7Zhps=";

  # The receiver's tests run git against a repository on disk.
  nativeCheckInputs = [ git ];

  env.CGO_ENABLED = 0;
  ldflags = [
    "-s"
    "-w"
  ];

  meta = {
    description = "The example site on small-press: builds it from example/content/";
    mainProgram = "example";
    license = lib.licenses.eupl12;
    platforms = lib.platforms.linux;
  };
}
