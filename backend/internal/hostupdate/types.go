package hostupdate

import "time"

type Phase string

const (
	PhaseIdle               Phase = "idle"
	PhaseChecking           Phase = "checking"
	PhaseReady              Phase = "ready"
	PhaseNoUpdate           Phase = "no_update"
	PhasePreflight          Phase = "preflight"
	PhaseBackingUp          Phase = "backing_up"
	PhasePulling            Phase = "pulling"
	PhaseDraining           Phase = "draining"
	PhaseMigrating          Phase = "migrating"
	PhaseSwitching          Phase = "switching"
	PhaseVerifying          Phase = "verifying"
	PhaseSucceeded          Phase = "succeeded"
	PhaseRollingBack        Phase = "rolling_back"
	PhaseRolledBack         Phase = "rolled_back"
	PhaseFailed             Phase = "failed"
	PhaseManualIntervention Phase = "manual_intervention"
)

func (p Phase) Active() bool {
	switch p {
	case PhasePreflight, PhaseBackingUp, PhasePulling, PhaseDraining, PhaseMigrating, PhaseSwitching, PhaseVerifying, PhaseRollingBack:
		return true
	default:
		return false
	}
}

type MigrationPhase string

const (
	MigrationPhaseIdle         MigrationPhase = "idle"
	MigrationPhaseValidating   MigrationPhase = "validating"
	MigrationPhaseBackingUp    MigrationPhase = "backing_up"
	MigrationPhaseStopping     MigrationPhase = "stopping"
	MigrationPhasePackaging    MigrationPhase = "packaging"
	MigrationPhaseUploading    MigrationPhase = "uploading"
	MigrationPhaseRestoring    MigrationPhase = "restoring"
	MigrationPhaseStarting     MigrationPhase = "starting"
	MigrationPhaseVerifying    MigrationPhase = "verifying"
	MigrationPhaseSucceeded    MigrationPhase = "succeeded"
	MigrationPhaseFailed       MigrationPhase = "failed"
	MigrationPhaseManualAction MigrationPhase = "manual_intervention"
)

func (p MigrationPhase) Active() bool {
	switch p {
	case MigrationPhaseValidating, MigrationPhaseBackingUp, MigrationPhaseStopping, MigrationPhasePackaging, MigrationPhaseUploading, MigrationPhaseRestoring, MigrationPhaseStarting, MigrationPhaseVerifying:
		return true
	default:
		return false
	}
}

type Release struct {
	Version     string    `json:"version"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"publishedAt"`
	Prerelease  bool      `json:"prerelease"`
}

type Check struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Blocking bool   `json:"blocking"`
}

type Backup struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Checksum  string    `json:"checksum"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
	Version   string    `json:"version"`
}

type LogEntry struct {
	At      time.Time `json:"at"`
	Phase   Phase     `json:"phase"`
	Message string    `json:"message"`
}

type Operation struct {
	ID                string     `json:"id,omitempty"`
	Phase             Phase      `json:"phase"`
	FromVersion       string     `json:"fromVersion,omitempty"`
	TargetVersion     string     `json:"targetVersion,omitempty"`
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	FinishedAt        *time.Time `json:"finishedAt,omitempty"`
	Error             string     `json:"error,omitempty"`
	RollbackError     string     `json:"rollbackError,omitempty"`
	AutomaticRollback bool       `json:"automaticRollback"`
	Logs              []LogEntry `json:"logs"`
}

type Status struct {
	Supported       bool            `json:"supported"`
	Connected       bool            `json:"connected"`
	Repository      string          `json:"repository"`
	Deployment      string          `json:"deployment"`
	CurrentVersion  string          `json:"currentVersion"`
	LatestRelease   *Release        `json:"latestRelease,omitempty"`
	UpdateAvailable bool            `json:"updateAvailable"`
	Checks          []Check         `json:"checks"`
	LastBackup      *Backup         `json:"lastBackup,omitempty"`
	RollbackVersion string          `json:"rollbackVersion,omitempty"`
	Operation       Operation       `json:"operation"`
	Migration       MigrationStatus `json:"migration"`
}

type StartRequest struct {
	TargetVersion string `json:"targetVersion"`
}

type RollbackRequest struct {
	Reason string `json:"reason"`
}

type MigrationArchive struct {
	ID             string    `json:"id"`
	Path           string    `json:"-"`
	Checksum       string    `json:"checksum"`
	Size           int64     `json:"size"`
	CreatedAt      time.Time `json:"createdAt"`
	Version        string    `json:"version"`
	DatabaseDriver string    `json:"databaseDriver"`
}

type MigrationLog struct {
	At      time.Time      `json:"at"`
	Phase   MigrationPhase `json:"phase"`
	Message string         `json:"message"`
}

type MigrationOperation struct {
	ID         string            `json:"id,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Phase      MigrationPhase    `json:"phase"`
	StartedAt  *time.Time        `json:"startedAt,omitempty"`
	FinishedAt *time.Time        `json:"finishedAt,omitempty"`
	Error      string            `json:"error,omitempty"`
	Archive    *MigrationArchive `json:"archive,omitempty"`
	Logs       []MigrationLog    `json:"logs"`
}

type MigrationStatus struct {
	Supported      bool               `json:"supported"`
	Reason         string             `json:"reason,omitempty"`
	MaxArchiveSize int64              `json:"maxArchiveSize"`
	LastExport     *MigrationArchive  `json:"lastExport,omitempty"`
	Operation      MigrationOperation `json:"operation"`
}
