package wiki

import (
	"fmt"
	"os"
	"strings"
)

// ReadScanMembers reads the <wiki-scan> block from the given wiki index.md
// file and returns the members listed in it.  This is the exported reader used
// by the code-intel realm seeder (CP3) to discover members and their statuses
// without running a full Scan.
//
// An absent file or a missing <wiki-scan> block returns (nil, nil) — not an
// error. The caller decides whether to seed from another source or error out.
// A genuine read failure (permissions, etc.) returns a non-nil error.
func ReadScanMembers(indexPath string) ([]Member, error) {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("wiki: read scan members: %w", err)
	}

	blockContent := extractBlockContent(string(data))
	if blockContent == "" {
		return nil, nil
	}

	var members []Member
	for _, line := range strings.Split(blockContent, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "<repo ") {
			continue
		}
		path := attrValue(line, "path")
		status := attrValue(line, "status")
		summary := attrValue(line, "summary")
		if path == "" || status == "" {
			continue
		}
		m := Member{
			Path:   path,
			Status: status,
		}
		if summary != "" {
			m.SummaryPath = summary
		}
		members = append(members, m)
	}
	return members, nil
}
