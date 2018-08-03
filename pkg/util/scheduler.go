/*
Copyright (C) 2018 Synopsys, Inc.

Licensed to the Apache Software Foundation (ASF) under one
or more contributor license agreements. See the NOTICE file
distributed with this work for additional information
regarding copyright ownership. The ASF licenses this file
to you under the Apache License, Version 2.0 (the
"License"); you may not use this file except in compliance
with the License. You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, either express or implied. See the License for the
specific language governing permissions and limitations
under the License.
*/

package util

import (
	"fmt"
	"time"
)

// SchedulerState describes the state of a scheduler
type SchedulerState int

// .....
const (
	SchedulerStateReady         SchedulerState = iota
	SchedulerStateRunningAction SchedulerState = iota
	SchedulerStatePaused        SchedulerState = iota
	SchedulerStateStopped       SchedulerState = iota
)

// String .....
func (state SchedulerState) String() string {
	switch state {
	case SchedulerStateReady:
		return "SchedulerStateReady"
	case SchedulerStateRunningAction:
		return "SchedulerStateRunningAction"
	case SchedulerStatePaused:
		return "SchedulerStatePaused"
	case SchedulerStateStopped:
		return "SchedulerStateStopped"
	}
	panic(fmt.Errorf("invalid SchedulerState value: %d", state))
}

type resume struct {
	runImmediately bool
	err            chan error
}

// Scheduler periodically executes `action`, with a pause of `delay` between
// invocations, and stops when receiving an event on `stop`.
type Scheduler struct {
	state     SchedulerState
	nextState SchedulerState
	delay     time.Duration
	action    func()
	timer     *time.Timer
	// channels
	pause           chan chan error
	resume          chan *resume
	runAction       chan bool
	didFinishAction chan bool
	stop            <-chan struct{}
	setDelay        chan time.Duration
}

// NewScheduler creates a new scheduler, which can immediately run 'action' or wait until 'delay' before
// running 'action' for the first time.
func NewScheduler(delay time.Duration, stop <-chan struct{}, runImmediately bool, action func()) *Scheduler {
	scheduler := &Scheduler{
		state:     SchedulerStateReady,
		delay:     delay,
		action:    action,
		pause:     make(chan chan error),
		resume:    make(chan *resume),
		runAction: make(chan bool),
		stop:      stop,
		setDelay:  make(chan time.Duration)}
	go scheduler.start()
	if runImmediately {
		scheduler.runAction <- true
	} else {
		scheduler.startTimer()
	}
	return scheduler
}

func (scheduler *Scheduler) start() {
	for {
		select {
		case <-scheduler.didFinishAction:
			switch scheduler.nextState {
			case SchedulerStateStopped:
				scheduler.state = SchedulerStateStopped
				return
			case SchedulerStatePaused:
				scheduler.state = SchedulerStatePaused
			default:
				scheduler.state = SchedulerStateReady
				scheduler.startTimer()
			}
		case <-scheduler.runAction:
			scheduler.nextState = SchedulerStateReady
			go func() {
				scheduler.action()
				scheduler.didFinishAction <- true
			}()
		case ch := <-scheduler.pause:
			switch scheduler.state {
			case SchedulerStateReady:
				scheduler.state = SchedulerStatePaused
				scheduler.stopTimer()
				ch <- nil
			case SchedulerStateRunningAction:
				// if nextState is Stopped or Paused, then error ...
				scheduler.nextState = SchedulerStatePaused
				ch <- nil
			default:
				ch <- fmt.Errorf("cannot pause scheduler while in state %s", scheduler.state.String())
			}
		case action := <-scheduler.resume:
			switch scheduler.state {
			case SchedulerStatePaused:
				scheduler.state = SchedulerStateReady
				action.err <- nil
				if action.runImmediately {
					scheduler.runAction <- true
				} else {
					scheduler.startTimer()
				}
			default:
				action.err <- fmt.Errorf("cannot resume scheduler while in state %s", scheduler.state.String())
			}
		case <-scheduler.stop:
			switch scheduler.state {
			case SchedulerStateReady:
				scheduler.stopTimer()
			case SchedulerStateRunningAction:
				scheduler.nextState = SchedulerStateStopped
			case SchedulerStatePaused:
				scheduler.state = SchedulerStateStopped
			case SchedulerStateStopped:
				// ??? not sure how this would happen
			}
			return
		case delay := <-scheduler.setDelay:
			scheduler.delay = delay
		}
	}
}

func (scheduler *Scheduler) startTimer() {
	scheduler.timer = time.NewTimer(scheduler.delay)
	go func() {
		// TODO does this correctly handle the case where the timer gets cancelled?
		_, ok := <-scheduler.timer.C
		if ok {
			scheduler.runAction <- true
		}
	}()
}

func (scheduler *Scheduler) stopTimer() {
	scheduler.timer.Stop()
}

// Pause temporarily stops the scheduler.
// It returns
func (scheduler *Scheduler) Pause() error {
	ch := make(chan error)
	scheduler.pause <- ch
	return <-ch
}

// Resume resumes the scheduler with the option of immediately running the action.
func (scheduler *Scheduler) Resume(runImmediately bool) error {
	action := &resume{runImmediately: runImmediately, err: make(chan error)}
	scheduler.resume <- action
	return <-action.err
}

// SetDelay sets the delay
func (scheduler *Scheduler) SetDelay(delay time.Duration) {
	scheduler.setDelay <- delay
}
