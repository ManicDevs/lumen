package app

import (
	"fmt"
	"os"
	"strings"

	"gitlab.torproject.org/cerberus-droid/lumen/internal/version"
)

// Flags represents the parsed command-line flags and positional arguments.
type Flags struct {
	AutoMode       bool   // --auto
	AutoGoal       string // value after --auto
	AutoSandbox    bool   // --auto-sandbox
	LiveOutput     bool   // --live-output
	EasterEgg      bool   // --easter-egg
	Continuous     bool   // --continuous / --autonomous
	PipeDataset    bool   // --pipe-dataset
	Train          bool   // --train / --train-all
	TrainAll       bool   // --train-all
	TrainLoRA      bool   // --train-lora
	LoRAAdapter    string // --lora-adapter <path>
	LoRAModel      string // --lora-model <name>
	DatasetInit    bool   // --dataset-init
	DatasetExport  bool   // --dataset-export
	ExportFormat   string // --export-format <sharegpt|alpaca|jsonl>
	ExportPath     string // --export-path <file>
	DatasetCurate  bool   // --dataset-curate
	CurateMinLen   int    // --curate-min-len
	CurateMaxLen   int    // --curate-max-len
	Eval           bool   // --eval
	EvalModel      string // --eval-model <name>
	EvalBaseModel  string // --eval-base-model <name>
	AutoLoop       bool   // --auto-loop
	LoopIterations int    // --loop-iterations N
	API            bool   // --api
	Chat           bool   // --chat
	CustomTopic    string // --topic <topic>
	TargetPath     string // positional: file or directory to analyse
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
		case "--version":
			fmt.Println(version.String())
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
		case "--train-lora":
			f.TrainLoRA = true
		case "--lora-adapter":
			if i+1 < len(args) {
				f.LoRAAdapter = args[i+1]
				i++
			}
		case "--lora-model":
			if i+1 < len(args) {
				f.LoRAModel = args[i+1]
				i++
			}
		case "--api":
			f.API = true
		case "--eval":
			f.Eval = true
		case "--eval-model":
			if i+1 < len(args) {
				f.EvalModel = args[i+1]
				i++
			}
		case "--eval-base-model":
			if i+1 < len(args) {
				f.EvalBaseModel = args[i+1]
				i++
			}
		case "--auto-loop":
			f.AutoLoop = true
		case "--loop-iterations":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &f.LoopIterations)
				i++
			}
		case "--dataset-init":
			f.DatasetInit = true
		case "--dataset-export":
			f.DatasetExport = true
		case "--export-format":
			if i+1 < len(args) {
				f.ExportFormat = args[i+1]
				i++
			}
		case "--export-path":
			if i+1 < len(args) {
				f.ExportPath = args[i+1]
				i++
			}
		case "--dataset-curate":
			f.DatasetCurate = true
		case "--curate-min-len":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &f.CurateMinLen)
				i++
			}
		case "--curate-max-len":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &f.CurateMaxLen)
				i++
			}
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
	fmt.Fprintf(os.Stderr, "Usage (Code Mode):       %s <target_path> [--auto-sandbox]\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Chat Mode):       %s --chat [--auto-sandbox] [--easter-egg] [--continuous] [--pipe-dataset] [--topic \"topic\"]\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Auto Mode):       %s --auto <goal> [--auto-sandbox] [--live-output]\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Train Mode):      %s --train | %s --train-all\n", progName, progName)
	fmt.Fprintf(os.Stderr, "Usage (LoRA Train Mode): %s --train-lora --lora-adapter <path> --lora-model <name>\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Eval Mode):       %s --eval --eval-model <name> [--eval-base-model <name>]\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Auto Loop Mode):  %s --auto-loop [--loop-iterations N] [--target-path <dir>]\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (API Mode):        %s --api\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Dataset Init):    %s --dataset-init\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Dataset Export):  %s --dataset-export --export-format <sharegpt|alpaca|jsonl> --export-path <file>\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Dataset Curate):  %s --dataset-curate [--curate-min-len N] [--curate-max-len N]\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Help):            %s --help\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Version):         %s --version\n", progName)
}
