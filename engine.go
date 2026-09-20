package main

import "errors"

// Everything runs on the Windows message thread. Timers wake it only when work
// is due; no worker threads, polling loop, or synchronization are needed.
type clickDriver interface {
	valid() bool
	button(down bool) error
	arm(milliseconds int) error
	disarm()
}

type clicker struct {
	driver                 clickDriver
	running, pressed, hold bool
	interval               int
}

func (c *clicker) start(interval int, hold bool) error {
	if c.running || c.pressed {
		return errors.New("Stop the current clicker first.")
	}
	if !c.driver.valid() {
		return errors.New("Target closed. Select another window.")
	}
	c.interval, c.hold, c.running = interval, hold, true
	if err := c.driver.button(true); err != nil {
		c.running = false
		return err
	}
	c.pressed = true
	wait := 10 // Match the original's 10 ms button-down, then configured delay.
	if hold {
		wait = 500
	} // Check target lifetime without spinning while held.
	return c.schedule(wait)
}

func (c *clicker) tick() error {
	if !c.running {
		return nil
	}
	if !c.driver.valid() {
		c.driver.disarm()
		c.running, c.pressed = false, false
		return errors.New("Target closed. Select another window.")
	}
	if c.hold {
		return c.schedule(500)
	}
	nextDown := !c.pressed
	if err := c.driver.button(nextDown); err != nil {
		_ = c.stop() // Also attempt release if a button-up could not be posted.
		return err
	}
	c.pressed = nextDown
	wait := c.interval
	if nextDown {
		wait = 10
	}
	return c.schedule(wait)
}

func (c *clicker) schedule(wait int) error {
	if err := c.driver.arm(wait); err != nil {
		_ = c.stop()
		return err
	}
	return nil
}

func (c *clicker) stop() error {
	c.driver.disarm()
	c.running = false
	if c.pressed && c.driver.valid() {
		if err := c.driver.button(false); err != nil {
			return err
		}
	}
	c.pressed = false
	return nil
}
