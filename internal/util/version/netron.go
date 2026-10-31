package version

import (
	"strconv"
	"strings"
)

const netronSuffix = "-netron."

// splitNetronRevision splits "3.11.0-netron.2" into "3.11.0" and 2; a plain
// version has revision 0. Any other suffix is not a version this parser orders.
func splitNetronRevision(v string) (base string, revision int, ok bool) {
	base, rev, found := strings.Cut(v, netronSuffix)
	if !found {
		return v, 0, !strings.Contains(v, "-")
	}
	n, err := strconv.Atoi(rev)
	if err != nil || n < 0 || strconv.Itoa(n) != rev {
		return "", 0, false
	}
	return base, n, true
}
