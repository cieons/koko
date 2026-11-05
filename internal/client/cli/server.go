package cli

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
)

// rootKeyCredential is the Root Key's credential in the koko unit,
// LoadCredential=root-key:/etc/koko/root.key, which systemd hands the service
// as a file of that name in $CREDENTIALS_DIRECTORY (P0 spec: install and run).
const rootKeyCredential = "root-key"

// defaultRootKeyFile is --root-key-file's default, as help shows it. Only this
// value is expanded: a path given is taken as it is, even one holding '$'.
const defaultRootKeyFile = "$CREDENTIALS_DIRECTORY/" + rootKeyCredential

var serverCommands = []Command{
	{
		Noun:    "server",
		Verb:    "run",
		Summary: "Run the koko server in the foreground, as its systemd unit does",
		Flags: []Flag{
			{
				Name:    "data-dir",
				Value:   "dir",
				Env:     "KOKO_DATA_DIR",
				Default: "/var/lib/koko",
				Summary: "the data directory, which holds koko.db",
			},
			{
				Name:    "root-key-file",
				Value:   "file",
				Env:     "KOKO_ROOT_KEY_FILE",
				Default: defaultRootKeyFile,
				Summary: "the Root Key's file, outside the data directory",
			},
			{
				Name:    "log-level",
				Value:   "level",
				Env:     "KOKO_LOG_LEVEL",
				Default: "info",
				Choices: []string{"debug", "info", "warn", "error"},
				Summary: "the lowest level the log on stderr holds",
			},
		},
		// It runs the koko server and migrates its database (P0 spec: overview).
		Tier: Irreversible,
		Run:  quietAction(serverRun),
	},
}

func serverRun(ctx context.Context, in *Input) error {
	if in.runner.ServerRun == nil {
		return &cliError{code: codeUnsupportedPlatform, message: fmt.Sprintf(
			"the koko server runs on Linux only; this koko is built for %s/%s", runtime.GOOS, runtime.GOARCH)}
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(in.Flag("log-level"))); err != nil {
		return fmt.Errorf("read the log level: %w", err)
	}
	rootKey := in.Flag("root-key-file")
	if rootKey == defaultRootKeyFile {
		dir := in.inv.getenv("CREDENTIALS_DIRECTORY")
		if dir == "" {
			return usagef("give the Root Key's file with --root-key-file or KOKO_ROOT_KEY_FILE: its default, %s, needs CREDENTIALS_DIRECTORY, which systemd sets for the koko unit", defaultRootKeyFile)
		}
		rootKey = filepath.Join(dir, rootKeyCredential)
	}
	return in.runner.ServerRun(ctx, ServerSettings{
		DataDir:     in.Flag("data-dir"),
		RootKeyPath: rootKey,
		LogLevel:    level,
	}, in.inv.Stderr)
}
