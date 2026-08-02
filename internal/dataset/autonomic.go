package dataset

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "regexp"
    "runtime"
    "strconv"
    "time"
)

// UniversalState handles autonomous scaling factors for any host platform
type UniversalState struct {
    AvailableCores   int     `json:"available_cores"`
    TargetOpsPerCore float64 `json:"target_ops_per_core"`
    CurrentThreads   int     `json:"current_threads"`
    ThrottleCount    int     `json:"throttle_count"`
}

// Live telemetry record designed for cross-platform log streaming

type TelemetryPoint struct {
    Timestamp      time.Time `json:"timestamp"`
    TotalCores     int       `json:"total_cores"`
    AllocatedThreads int     `json:"allocated_threads"`
    OpsPerCore     float64   `json:"ops_per_core"`
    Status         string    `json:"status"`
    RequiresTuning bool      `json:"requires_tuning"`
}

var SystemProfile *UniversalState

func init() {
    // Dynamically query the physical host architecture at boot
    cores := runtime.NumCPU()
    SystemProfile = &UniversalState{
        AvailableCores:   cores,
        TargetOpsPerCore: 350.0, // Absolute baseline score per single core execution thread
        CurrentThreads:   cores,
        ThrottleCount:    0,
    }
    runtime.GOMAXPROCS(cores)
}

// ExecuteUniversalDiagnostic dynamically benchmarks whatever system it is currently running on
func ExecuteUniversalDiagnostic(ctx context.Context) (*TelemetryPoint, error) {
    // Spawn go bench natively using the current runtime's detected core metric
    coreStr := strconv.Itoa(SystemProfile.CurrentThreads)
    cmd := exec.CommandContext(ctx, "go", "test", "./internal/app/...", "-bench=.", "-cpu="+coreStr)
    var out bytes.Buffer
    cmd.Stdout = &out

    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("cross-platform benchmark error: %w", err)
    }

    // Regexp matches any core profile string dynamically (e.g., BenchmarkHarvestSimulation-4, -8, -64)
    re := regexp.MustCompile(`BenchmarkHarvestSimulation-\d+\s+(\d+)\s+ns/op`)
    matches := re.FindStringSubmatch(out.String())

    var nsPerOp float64
    if len(matches) > 1 {
        if parsed, err := strconv.ParseFloat(matches[1], 64); err == nil {
            nsPerOp = parsed
        }
    }

    var totalOpsPerSec float64
    if nsPerOp > 0 {
        totalOpsPerSec = 1000000000.0 / nsPerOp
    }

    // Calculate throughput per core to standardise scores across vastly different CPU families
    opsPerCore := totalOpsPerSec / float64(SystemProfile.CurrentThreads)

    requiresTuning := false
    status := "System_Nominal"

    if opsPerCore < SystemProfile.TargetOpsPerCore && opsPerCore > 0 {
        status = "Core_Saturation_Deficit"
        requiresTuning = true
    }

    return &TelemetryPoint{
        Timestamp:        time.Now().UTC(),
        TotalCores:       SystemProfile.AvailableCores,
        AllocatedThreads: SystemProfile.CurrentThreads,
        OpsPerCore:       opsPerCore,
        Status:           status,
        RequiresTuning:   requiresTuning,
    }, nil
}

// StartUniversalMonitor tracks host hardware health continuously on a background thread loop
func StartUniversalMonitor(ctx context.Context, projectRoot string) {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    fmt.Printf("[Lumen] Universal Autonomic Engine active. Probing hardware grid (%d threads available)...\n", SystemProfile.AvailableCores)

    for {
        select {
        case <-ticker.C:
            point, err := ExecuteUniversalDiagnostic(ctx)
            if err != nil {
                continue
            }

            // Write metrics directly into the structured datasets tree
            _ = logTelemetryToDisk(projectRoot, point)

            // Make closed-loop scaling adaptations on the fly
            OptimizeRuntimePrimitives(point)

        case <-ctx.Done():
            return
        }
    }
}

func OptimizeRuntimePrimitives(point *TelemetryPoint) {
    if point.RequiresTuning {
        SystemProfile.ThrottleCount++
        
        // If core execution is choking over multiple cycles, reduce core thread pressure to optimize cache lines
        if SystemProfile.ThrottleCount >= 2 && SystemProfile.CurrentThreads > 2 {
            SystemProfile.CurrentThreads = SystemProfile.CurrentThreads - 2
            runtime.GOMAXPROCS(SystemProfile.CurrentThreads)
            fmt.Printf("[Lumen] Throttling mitigated. Downscaled runtime execution mesh to %d threads.\n", SystemProfile.CurrentThreads)
        }
    } else {
        // Safe scaling recovery: expand back out to full system capability if processing is smooth
        if SystemProfile.CurrentThreads < SystemProfile.AvailableCores {
            SystemProfile.CurrentThreads = SystemProfile.AvailableCores
            runtime.GOMAXPROCS(SystemProfile.CurrentThreads)
            fmt.Printf("[Lumen] Hardware grid stable. Restored max runtime allocation to %d threads.\n", SystemProfile.CurrentThreads)
        }
        SystemProfile.ThrottleCount = 0
    }
}

func logTelemetryToDisk(root string, point *TelemetryPoint) error {
    logPath := filepath.Join(root, "data", "datasets", "telemetry")
    if err := os.MkdirAll(logPath, 0755); err != nil {
        return err
    }
    
    f, err := os.OpenFile(filepath.Join(logPath, "universal_telemetry.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        return err
    }
    defer f.Close()

    data, _ := json.Marshal(point)
    _, err = f.Write(append(data, '\n'))
    return err
}
