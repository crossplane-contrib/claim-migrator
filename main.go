package main

import (
	"github.com/alecthomas/kong"
	"github.com/crossplane/crossplane-runtime/pkg/logging"

	"github.com/crossplane-contrib/claim-migrator/migrate"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var _ = kong.Must(&cli{})

type (
	debugFlag bool
)

func (d debugFlag) BeforeApply(ctx *kong.Context) error { //nolint:unparam // BeforeApply requires this signature.
	logger := logging.NewLogrLogger(zap.New(zap.UseDevMode(true)))
	ctx.BindTo(logger, (*logging.Logger)(nil))
	return nil
}

type cli struct {
	Migrate migrate.Cmd `cmd:"" help:"Migrate Crossplane Claims to a new namespace."`

	Debug debugFlag `short:"d" optional:"" help:"(Optional) Verbose logging."`
}

func main() {
	logger := logging.NewLogrLogger(zap.New())
	ctx := kong.Parse(&cli{},
		kong.Name("claim"),
		kong.Description("A command line tool to manage Crossplane Claims"),
		// Binding a variable to kong context makes it available to all commands
		// at runtime.
		kong.BindTo(logger, (*logging.Logger)(nil)),
		kong.ConfigureHelp(kong.HelpOptions{
			FlagsLast:      true,
			Compact:        true,
			WrapUpperBound: 80,
		}),
		kong.UsageOnError())
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
