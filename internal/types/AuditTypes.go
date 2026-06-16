package Types

type AuditEntry struct {
	Purpose   string
	User      string
	ID        string
	Agent     string
	BlockList []string
	Files     []string
	Approved  bool
	TestPass  bool
	Timestamp string
}
