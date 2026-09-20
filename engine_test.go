package main

import (
	"errors"
	"reflect"
	"testing"
)

type fakeDriver struct {
	alive                       bool
	events                      []bool
	delays                      []int
	armed                       bool
	failDown, failUp, failTimer bool
}

func (d *fakeDriver) valid() bool { return d.alive }
func (d *fakeDriver) button(down bool) error {
	if down && d.failDown || !down && d.failUp {
		return errors.New("input failed")
	}
	d.events = append(d.events, down)
	return nil
}
func (d *fakeDriver) arm(delay int) error {
	if d.failTimer {
		return errors.New("timer failed")
	}
	d.delays = append(d.delays, delay)
	d.armed = true
	return nil
}
func (d *fakeDriver) disarm() { d.armed = false }

func TestClickCadenceAndStop(t *testing.T) {
	d := &fakeDriver{alive: true}
	c := clicker{driver: d}
	if err := c.start(50, false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := c.tick(); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.stop(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.events, []bool{true, false, true, false}) {
		t.Fatal(d.events)
	}
	if !reflect.DeepEqual(d.delays, []int{10, 50, 10, 50}) {
		t.Fatal(d.delays)
	}
	if c.running || c.pressed || d.armed {
		t.Fatal("stop left work active")
	}
	_ = c.tick() // Stale queued timer after stop must not click.
	if len(d.events) != 4 {
		t.Fatal("stale timer clicked")
	}
}

func TestHoldAndRapidRestartRelease(t *testing.T) {
	d := &fakeDriver{alive: true}
	c := clicker{driver: d}
	for i := 0; i < 100; i++ {
		if err := c.start(20, true); err != nil {
			t.Fatal(err)
		}
		for tick := 0; tick < 4; tick++ {
			if err := c.tick(); err != nil {
				t.Fatal(err)
			}
		}
		if len(d.events) != i*2+1 {
			t.Fatal("hold repeated mouse down")
		}
		if err := c.stop(); err != nil {
			t.Fatal(err)
		}
	}
	for i, event := range d.events {
		if event != (i%2 == 0) {
			t.Fatal("unbalanced mouse input")
		}
	}
}

func TestFailureCleanup(t *testing.T) {
	for _, scenario := range []string{"target closed", "mouse down denied", "timer failed", "release retry"} {
		t.Run(scenario, func(t *testing.T) {
			d := &fakeDriver{alive: true}
			c := clicker{driver: d}
			switch scenario {
			case "target closed":
				_ = c.start(50, true)
				d.alive = false
				if c.tick() == nil {
					t.Fatal("target loss was not reported")
				}
			case "mouse down denied":
				d.failDown = true
				if c.start(50, false) == nil {
					t.Fatal("access failure was not reported")
				}
			case "timer failed":
				d.failTimer = true
				if c.start(50, true) == nil {
					t.Fatal("timer failure was not reported")
				}
				if !reflect.DeepEqual(d.events, []bool{true, false}) {
					t.Fatal("mouse wasn't released", d.events)
				}
			case "release retry":
				_ = c.start(50, true)
				d.failUp = true
				if c.stop() == nil || !c.pressed {
					t.Fatal("failed release should be retryable")
				}
				if c.start(50, true) == nil {
					t.Fatal("restarted before release")
				}
				d.failUp = false
				if err := c.stop(); err != nil {
					t.Fatal(err)
				}
			}
			if c.running || c.pressed || d.armed {
				t.Fatal("failure left work active")
			}
		})
	}
}
