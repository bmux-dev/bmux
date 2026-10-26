package client

import "github.com/josephferrero/bmux/internal/protocol"

// sessionLine is one line of the sidebar or picker session list: a project
// header or a session.
type sessionLine struct {
	header  string           // project name; set only on header lines
	session protocol.Session // set on session lines
	nested  bool             // shown under a header
	branch  string           // checked-out branch, for sessions under a header
	index   int              // position among session lines, -1 on headers
}

// groupLines arranges sessions, already in display order, into project
// groups. Sessions in worktrees of one repository, and any session in that
// repository itself, follow a header named after it; the group sits where its
// first session was. Other sessions stay where they are, ungrouped. Order
// within a group is the input order.
func groupLines(sessions []protocol.Session, info func(dir string) repoInfo) []sessionLine {
	infos := make([]repoInfo, len(sessions))
	grouped := make(map[string]bool) // repositories with a worktree session
	for i, s := range sessions {
		if s.Dir != "" {
			infos[i] = info(s.Dir)
		}
		if infos[i].Worktree {
			grouped[infos[i].Root] = true
		}
	}
	lines := make([]sessionLine, 0, len(sessions)+len(grouped))
	emitted := make(map[string]bool)
	count := 0 // session lines so far
	add := func(l sessionLine) {
		l.index = -1
		if l.header == "" {
			l.index = count
			count++
		}
		lines = append(lines, l)
	}
	for i, s := range sessions {
		root := infos[i].Root
		if !grouped[root] {
			add(sessionLine{session: s})
			continue
		}
		if emitted[root] {
			continue
		}
		emitted[root] = true
		add(sessionLine{header: projectName(root)})
		for j, member := range sessions {
			if infos[j].Root == root {
				add(sessionLine{session: member, nested: true, branch: infos[j].Branch})
			}
		}
	}
	return lines
}

// groupedSessions is the sessions of groupLines in display order, without
// headers.
func groupedSessions(sessions []protocol.Session, info func(dir string) repoInfo) []protocol.Session {
	var result []protocol.Session
	for _, l := range groupLines(sessions, info) {
		if l.header == "" {
			result = append(result, l.session)
		}
	}
	return result
}
