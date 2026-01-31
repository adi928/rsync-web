package main

import (
	"log"
	"time"

	"github.com/robfig/cron/v3"
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
		log.Println("scheduled backup triggered")
		if err := executor.Run(); err != nil {
			log.Printf("scheduled backup skipped: %v", err)
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
	log.Printf("scheduler started with schedule: %s", s.schedule)
}

func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Println("scheduler stopped")
}

// NextRun returns the next scheduled backup time.
func (s *Scheduler) NextRun() time.Time {
	entry := s.cron.Entry(s.entryID)
	return entry.Next
}
