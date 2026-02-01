package main

import (
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
)

type Scheduler struct {
	cron     *cron.Cron
	executor *BackupExecutor
	schedule string
	entryID  cron.EntryID
}

func NewScheduler(executor *BackupExecutor, schedule string) (*Scheduler, error) {
	c := cron.New()

	s := &Scheduler{
		cron:     c,
		executor: executor,
		schedule: schedule,
	}

	id, err := c.AddFunc(schedule, func() {
		log.Info().Msg("scheduled backup triggered")
		if err := executor.Run(); err != nil {
			log.Warn().Err(err).Msg("scheduled backup skipped")
		}
	})
	if err != nil {
		return nil, err
	}
	s.entryID = id

	return s, nil
}

func (s *Scheduler) Start() {
	s.cron.Start()
	log.Info().Str("schedule", s.schedule).Msg("scheduler started")
}

func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Info().Msg("scheduler stopped")
}

// NextRun returns the next scheduled backup time.
func (s *Scheduler) NextRun() time.Time {
	entry := s.cron.Entry(s.entryID)
	return entry.Next
}
