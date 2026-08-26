package dispatch

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/fault"
)

type JobStatus string

const (
	JobPending          JobStatus = "pending"
	JobLeased           JobStatus = "leased"
	JobSucceeded        JobStatus = "succeeded"
	JobRetryable        JobStatus = "retryable"
	JobPermanentFailure JobStatus = "permanent_failure"
	JobCancelled        JobStatus = "cancelled"
)

type Job struct {
	ID            string
	PlanID        string
	ClusterID     string
	Status        JobStatus
	Attempts      int
	MaxAttempts   int
	NextAttemptAt time.Time
	LeaseOwner    string
	LeaseUntil    *time.Time
	LastError     string
	CommandKey    string
	Version       int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewJob(planID, clusterID string, maxAttempts int, now time.Time) (Job, error) {
	if strings.TrimSpace(planID) == "" || strings.TrimSpace(clusterID) == "" {
		return Job{}, fault.New(fault.Invalid, "missing_job_owner", "plan and cluster are required")
	}
	if maxAttempts < 1 || maxAttempts > 20 {
		return Job{}, fault.New(fault.Invalid, "invalid_attempt_limit", "attempt limit must be between 1 and 20")
	}
	now = now.UTC()
	id := uuid.NewString()
	return Job{
		ID:            id,
		PlanID:        planID,
		ClusterID:     clusterID,
		Status:        JobPending,
		MaxAttempts:   maxAttempts,
		NextAttemptAt: now,
		CommandKey:    fmt.Sprintf("dispatch:%s:%s", planID, clusterID),
		Version:       1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func (j Job) Claimable(now time.Time) bool {
	now = now.UTC()
	switch j.Status {
	case JobPending, JobRetryable:
		return !j.NextAttemptAt.After(now)
	case JobLeased:
		return j.LeaseUntil != nil && !j.LeaseUntil.After(now)
	default:
		return false
	}
}

func (j Job) Lease(owner string, now time.Time, duration time.Duration) (Job, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return Job{}, fault.New(fault.Invalid, "lease_owner_required", "lease owner is required")
	}
	if duration < time.Second || duration > 10*time.Minute {
		return Job{}, fault.New(fault.Invalid, "invalid_lease_duration", "lease duration is outside policy")
	}
	if !j.Claimable(now) {
		return Job{}, fault.New(fault.Conflict, "job_not_claimable", "execution job is not ready for a lease")
	}
	until := now.UTC().Add(duration)
	copy := j
	copy.Status = JobLeased
	copy.LeaseOwner = owner
	copy.LeaseUntil = &until
	copy.Attempts++
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (j Job) Renew(owner string, now time.Time, duration time.Duration) (Job, error) {
	if j.Status != JobLeased || j.LeaseOwner != owner || j.LeaseUntil == nil {
		return Job{}, fault.New(fault.Conflict, "lease_not_owned", "worker does not own this job lease")
	}
	if !now.UTC().Before(*j.LeaseUntil) {
		return Job{}, fault.New(fault.Conflict, "lease_expired", "job lease already expired")
	}
	until := now.UTC().Add(duration)
	copy := j
	copy.LeaseUntil = &until
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (j Job) Succeed(owner string, now time.Time) (Job, error) {
	if err := j.verifyLease(owner, now); err != nil {
		return Job{}, err
	}
	copy := j
	copy.Status = JobSucceeded
	copy.LeaseOwner = ""
	copy.LeaseUntil = nil
	copy.LastError = ""
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (j Job) Fail(owner string, cause error, retryable bool, now time.Time, baseBackoff time.Duration) (Job, error) {
	if err := j.verifyLease(owner, now); err != nil {
		return Job{}, err
	}
	if cause == nil {
		return Job{}, fault.New(fault.Invalid, "job_error_required", "job failure needs an error")
	}
	copy := j
	copy.LeaseOwner = ""
	copy.LeaseUntil = nil
	copy.LastError = cause.Error()
	if retryable && copy.Attempts < copy.MaxAttempts {
		copy.Status = JobRetryable
		copy.NextAttemptAt = now.UTC().Add(Backoff(baseBackoff, copy.Attempts))
	} else {
		copy.Status = JobPermanentFailure
	}
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (j Job) Cancel(now time.Time) (Job, error) {
	if j.Status == JobSucceeded || j.Status == JobPermanentFailure {
		return Job{}, fault.New(fault.Conflict, "terminal_job", "terminal execution job cannot be cancelled")
	}
	copy := j
	copy.Status = JobCancelled
	copy.LeaseOwner = ""
	copy.LeaseUntil = nil
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}

func (j Job) verifyLease(owner string, now time.Time) error {
	if j.Status != JobLeased || j.LeaseOwner != owner || j.LeaseUntil == nil {
		return fault.New(fault.Conflict, "lease_not_owned", "worker does not own this job lease")
	}
	if now.UTC().After(*j.LeaseUntil) {
		return fault.New(fault.Conflict, "lease_expired", "job lease expired before completion")
	}
	return nil
}

func Backoff(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 8 {
		shift = 8
	}
	value := base * time.Duration(1<<shift)
	if value > 15*time.Minute {
		return 15 * time.Minute
	}
	return value
}

type JobSummary struct {
	Total            int
	Pending          int
	Leased           int
	Succeeded        int
	Retryable        int
	PermanentFailure int
	Cancelled        int
}

func SummarizeJobs(jobs []Job) JobSummary {
	result := JobSummary{Total: len(jobs)}
	for _, job := range jobs {
		switch job.Status {
		case JobPending:
			result.Pending++
		case JobLeased:
			result.Leased++
		case JobSucceeded:
			result.Succeeded++
		case JobRetryable:
			result.Retryable++
		case JobPermanentFailure:
			result.PermanentFailure++
		case JobCancelled:
			result.Cancelled++
		}
	}
	return result
}
