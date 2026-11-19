# Prints "<name> <version>" for the first chart version in a Helm repository
# index whose chart URL is relative, that is, one Depsilo rewrote so that the
# download resolves against the repository URL the client added.
function value(line) {
	sub(/^[^:]*:[ \t]*/, "", line)
	gsub(/["']/, "", line)
	return line
}

/^  - / { name = ""; version = ""; relative = 0; in_urls = 0 }

{
	line = $0
	sub(/^  - /, "    ", line)
}

line ~ /^    urls:[ \t]*$/ { in_urls = 1; next }
in_urls && line ~ /^    - / {
	if (line !~ /:\/\//) relative = 1
	next
}
line ~ /^    [A-Za-z]/ { in_urls = 0 }
line ~ /^    name:/ { name = value(line) }
line ~ /^    version:/ { version = value(line) }

relative && name != "" && version != "" { print name, version; exit }
