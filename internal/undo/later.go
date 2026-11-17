package undo

import "github.com/hkdb/aerion/internal/triage"

type LaterContext interface {
	RestoreLaterMembership(triage.Ref, triage.LaterState, triage.LaterState) error
}
type LaterChangeCommand struct {
	BaseCommand
	ctx           LaterContext
	ref           triage.Ref
	before, after triage.LaterState
}

func NewLaterChangeCommand(ctx LaterContext, ref triage.Ref, before, after triage.LaterState, description string) *LaterChangeCommand {
	return &LaterChangeCommand{BaseCommand: NewBaseCommand(description), ctx: ctx, ref: ref, before: before, after: after}
}
func (c *LaterChangeCommand) Execute() error { return nil }
func (c *LaterChangeCommand) Undo() error {
	return c.ctx.RestoreLaterMembership(c.ref, c.after, c.before)
}
