package scheduler

import (
	"log"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	c *cron.Cron
}

func New() *Scheduler {
	return &Scheduler{c: cron.New()}
}

func (s *Scheduler) Add(spec string, fn func()) error {
	_, err := s.c.AddFunc(spec, fn)
	if err != nil {
		return err
	}
	log.Printf("scheduler: registered job %q", spec)
	return nil
}

func (s *Scheduler) Start() {
	s.c.Start()
	log.Println("scheduler: started")
}

func (s *Scheduler) Stop() {
	s.c.Stop()
}
