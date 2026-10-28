# The site, as far as the module sees it: pages, and the nginx includes the
# real build writes. A page saying "fail" is a commit that doesn't build.
if grep -q fail "$page"; then
  echo "this commit doesn't build" >&2
  exit 1
fi
mkdir -p "$out/log/hello" "$out/nginx/server" "$out/nginx/location"
echo "<p>$(cat "$page")</p>" > "$out/index.html"
echo "<p>hello</p>" > "$out/log/hello/index.html"
echo "<p>not here</p>" > "$out/404.html"
printf 'User-agent: GPTBot\nDisallow: /\n' > "$out/robots.txt"
echo 'location = /log/old/ { return 301 /log/hello/; }' > "$out/nginx/server/aliases.conf"
echo 'if ($http_user_agent ~* "(GPTBot)") { return 403; }' > "$out/nginx/location/crawlers.conf"
