package Types

type AuditEntry struct {
	Purpose   string
	User      string
	ID        string
	Agent     string
	BlockList []string
	Files     []string
	TestPass  bool
	Overridden bool
	Timestamp string
}
