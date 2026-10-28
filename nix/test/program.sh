# The program, as far as the module sees it: something to run.
mkdir -p "$out/bin"
printf '#!/bin/sh\nexec sleep 1000\n' > "$out/bin/stand-in"
chmod +x "$out/bin/stand-in"
