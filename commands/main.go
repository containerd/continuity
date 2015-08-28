package commands

import (
	"io"
	"io/ioutil"
	"os"
	"text/tabwriter"

	"github.com/golang/protobuf/proto"
	"github.com/spf13/cobra"
	pb "github.com/stevvooe/continuity/proto"
)

var (
	MainCmd = &cobra.Command{
		Use:   "continuity <command>",
		Short: "A transport-agnostic filesytem metadata tool.",
	}

	// usageTemplate is nearly identical to the default template without the
	// automatic addition of flags. Instead, Command.Use is used unmodified.
	usageTemplate = `{{ $cmd := . }}
Usage: {{if .Runnable}}
  {{.UseLine}}{{end}}{{if gt .Aliases 0}}

Aliases:
  {{.NameAndAliases}}
{{end}}{{if .HasExample}}

Examples:
{{ .Example }}{{end}}{{ if .HasNonHelpSubCommands}}

Available Commands: {{range .Commands}}{{if (not .IsHelpCommand)}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{ if .HasLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages}}{{end}}{{ if .HasInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics: {{range .Commands}}{{if .IsHelpCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}}{{end}}{{end}}{{ if .HasSubCommands }}

Use "{{.CommandPath}} [command] --help" for more information about a command.
{{end}}`
)

func init() {
	MainCmd.AddCommand(ApplyCmd)
	MainCmd.AddCommand(BuildCmd)
	MainCmd.AddCommand(LSCmd)
	MainCmd.AddCommand(StatsCmd)
	MainCmd.AddCommand(DumpCmd)
	MainCmd.SetUsageTemplate(usageTemplate)
}

// readManifestFile reads the manifest from the given path. This should
// probably be provided by the continuity library.
func readManifestFile(path string) (*pb.Manifest, error) {
	p, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var bm pb.Manifest

	if err := proto.Unmarshal(p, &bm); err != nil {
		return nil, err
	}

	return &bm, nil
}

// newTabwriter provides a common tabwriter with defaults.
func newTabwriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
}
