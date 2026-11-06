package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hkdb/aerion/internal/platform"
)

func TestValidateOpenPath(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}

	paths := &platform.Paths{
		Data: dataDir,
	}
	attachmentsDir := paths.AttachmentsPath()
	if err := os.MkdirAll(attachmentsDir, 0700); err != nil {
		t.Fatalf("failed to create attachments dir: %v", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get user home dir: %v", err)
	}
	downloadsDir := filepath.Join(homeDir, "Downloads")

	app := &App{
		paths: paths,
	}

	validAttachment := filepath.Join(attachmentsDir, "document.pdf")
	validDownload := filepath.Join(downloadsDir, "report.docx")
	validDataFile := filepath.Join(dataDir, "export.zip")

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid attachment path",
			path:    validAttachment,
			wantErr: false,
		},
		{
			name:    "valid download path",
			path:    validDownload,
			wantErr: false,
		},
		{
			name:    "valid data path",
			path:    validDataFile,
			wantErr: false,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
		{
			name:    "whitespace path",
			path:    "   ",
			wantErr: true,
		},
		{
			name:    "leading dash flag injection",
			path:    "--help",
			wantErr: true,
		},
		{
			name:    "leading dash option injection",
			path:    "-oProxyCommand=calc",
			wantErr: true,
		},
		{
			name:    "null byte injection",
			path:    validAttachment + "\x00.evil",
			wantErr: true,
		},
		{
			name:    "newline injection",
			path:    validAttachment + "\ninjected",
			wantErr: true,
		},
		{
			name:    "path traversal outside allowed root",
			path:    filepath.Join(attachmentsDir, "../../../etc/passwd"),
			wantErr: true,
		},
		{
			name:    "system path outside allowed roots",
			path:    "/etc/shadow",
			wantErr: true,
		},
		{
			name:    "path component leading dash",
			path:    filepath.Join(attachmentsDir, "-flag", "doc.pdf"),
			wantErr: true,
		},
		{
			name:    "path component leading dash option",
			path:    filepath.Join(downloadsDir, "-oProxyCommand=calc", "doc.pdf"),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := app.validateOpenPath(tc.path)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateOpenPath(%q) error = %v, wantErr = %v", tc.path, err, tc.wantErr)
			}
		})
	}
}
