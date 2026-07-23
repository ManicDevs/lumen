package app

import (
	"fmt"
	"os"
	"strings"
)

type Flags struct {
	AutoMode    bool
	AutoGoal    string
	AutoSandbox bool
	LiveOutput  bool
	EasterEgg   bool
	Continuous  bool
	PipeDataset bool
	Train       bool
	TrainAll    bool
	DatasetInit bool
	Chat        bool
	CustomTopic string
	TargetPath  string
}

func ParseFlags(args []string) Flags {
	var f Flags

	for i := 0; i < len(args); i++ {
		switch args[i] {
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

func PrintUsage() {
	fmt.Fprintf(os.Stderr, "Usage (Code Mode):   %s <target_path> [--auto-sandbox]\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Chat Mode):   %s --chat [--auto-sandbox] [--easter-egg] [--continuous] [--pipe-dataset] [--topic \"topic\"]\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Auto Mode):   %s --auto <goal> [--auto-sandbox] [--live-output]\n", progName)
	fmt.Fprintf(os.Stderr, "Usage (Train Mode):  %s --train | %s --train-all\n", progName, progName)
	fmt.Fprintf(os.Stderr, "Usage (Dataset Mode): %s --dataset-init\n", progName)
}
