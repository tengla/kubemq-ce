package config

import "path/filepath"

// AuditConfig turns on the audit log: one JSON line per client request
// saying who did what on which channel, and whether it was allowed.
type AuditConfig struct {
	Enable bool `json:"enable"`
	// FilePath is the file the lines are appended to. Empty writes them to
	// the server log.
	FilePath string `json:"filePath"`
}

func defaultAuditConfig() *AuditConfig {
	bindViperEnv(
		"Audit.Enable",
		"Audit.FilePath",
	)
	return &AuditConfig{
		Enable:   false,
		FilePath: "",
	}
}

func (a *AuditConfig) Validate() error {
	if !a.Enable || a.FilePath == "" {
		return nil
	}
	if err := validateFileName(filepath.Dir(a.FilePath)); err != nil {
		return NewConfigurationErrorf("bad audit configuration: %s", err.Error())
	}
	return nil
}
