package app

import (
	"fmt"
	"os"
	"strings"
)

// Flags represents the parsed command-line flags and positional arguments.
// Flags represents the parsed command-line flags and positional arguments.
type Flags struct {
	AutoMode    bool   // --auto
	AutoGoal    string // value after --auto
	AutoSandbox bool   // --auto-sandbox
	LiveOutput  bool   // --live-output
	EasterEgg   bool   // --easter-egg
	Continuous  bool   // --continuous / --autonomous
	PipeDataset bool   // --pipe-dataset
	Train       bool   // --train / --train-all
	TrainAll    bool   // --train-all
	DatasetInit bool   // --dataset-init
	Chat        bool   // --chat
	CustomTopic string // --topic <topic>
	TargetPath  string // positional: file or directory to analyse
}

// ParseFlags parses command-line arguments into a Flags struct. It supports
// both long-form flags (--auto, --live-output, etc.) and a positional target
// path. Unknown flags are silently ignored; --help/-h prints usage and exits.
func ParseFlags(args []string) Flags {
	var f Flags

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--help", "-h":
			PrintUsage()
			os.Exit(0)
		case "--auto":
			f.AutoMode = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				f.AutoGoal = args[i+1]
				i++
			}
		case "--live-output":
			f.LiveOutput = true
		case "--easter-egg":
			f.EasterEgg = true
		case "--train":
			f.Train = true
		case "--train-all":
			f.Train = true
			f.TrainAll = true
		case "--dataset-init":
			f.DatasetInit = true
		case "--auto-sandbox":
			f.AutoSandbox = true
		case "--continuous", "--autonomous":
			f.Continuous = true
		case "--pipe-dataset":
			f.PipeDataset = true
		case "--chat":
			f.Chat = true
		case "--topic":
			if i+1 < len(args) {
				f.CustomTopic = args[i+1]
				i++
			}
		default:
			if !strings.HasPrefix(args[i], "--") && f.TargetPath == "" {
				f.TargetPath = args[i]
			}
		}
	}
	return f
}

// PrintUsage writes the full usage text to stderr listing every available
// mode and its flags.
func PrintUsage() {
	fmt.Fprintf(os.Stderr, "Usage (Code Mode):   %s <target_path> [--auto-sandbox]\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Chat Mode):   %s --chat [--auto-sandbox] [--easter-egg] [--continuous] [--pipe-dataset] [--topic \"topic\"]\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Auto Mode):   %s --auto <goal> [--auto-sandbox] [--live-output]\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Train Mode):  %s --train | %s --train-all\n", progName, progName)
	fmt.Fprintf(os.Stderr, "Usage (Dataset Mode): %s --dataset-init\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Help):        %s --help\n", progName)
}
