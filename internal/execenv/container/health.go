package container

// Proving a service is up before the loop's first command runs against it
// (EV14). Two shapes, both bounded and both captured through the daemon's probe
// door — the driver has no second path to stdout, and a health check that
// invents one is the leak this slice exists to close.

import (
	"context"
	"os/exec"
	"time"

	"github.com/cjlapao/aeon/internal/execenv"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

const (
	defaultHealthSeconds = 30
	maxHealthSeconds     = 300
	healthRetryEvery     = time.Second
)

// ProbeRunner is the daemon's door for running a prepared probe command: it
// mints the policy token for the probe (a probe is the DAEMON's action, not the
// agent's, and it goes through the same gate as everything else) and hands the
// result back. Nil means the driver refuses any service that declares a health
// check rather than pretending it passed.
type ProbeRunner func(ctx context.Context, cmd *exec.Cmd, command string) (tools.RunResult, *execenv.Refusal)

// WaitHealthy blocks until the service answers, or returns the refusal that says
// why the loop will not start. A zero timeout is the 30-second default and 300
// is the ceiling: a health check that never ends is a step that never ends.
func (p *Provisioner) WaitHealthy(ctx context.Context, svc execenv.Service, row store.Provision, network, workDir string) *execenv.Refusal {
	h := svc.Health
	if h == nil {
		return nil
	}
	if row.State != store.ProvisionStateReady {
		return nil // never started; the caller's failure path owns the sentence
	}
	seconds := h.TimeoutSeconds
	if seconds <= 0 {
		seconds = defaultHealthSeconds
	}
	if seconds > maxHealthSeconds {
		seconds = maxHealthSeconds
	}
	if p.Probe == nil {
		return execenv.NewRefusal(execenv.RefusalEnvironmentUnavailable,
			"the service "+svc.Name+" declares a health check but the daemon has not wired the probe door",
			"a health check runs a command and reads its output, which happens in exactly one place behind a policy "+
				"token; the driver does not capture output itself (EV10, EV12)",
			"this is a daemon build fault — report it; a loop is not started against an unproven service")
	}
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	var last string
	for {
		ok, probeErr := p.probeOnce(ctx, svc, row, network, workDir)
		if ok {
			return nil
		}
		last = probeErr
		if time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			break
		case <-time.After(healthRetryEvery):
		}
	}
	// The row settles as failed AND the answer is the spec's own kind: a person
	// reading the step must see `service-unhealthy`, not the generic
	// provision-failed that every other half-build produces (EV14).
	reason := "the service " + svc.Name + " did not become healthy within " + intString(seconds) +
		"s · last probe: " + orText(last, "no answer")
	if _, _, err := p.Ledger.MarkProvisionFailed(row.ID, reason); err != nil {
		reason = reason + " · and the ledger refused the failure: " + err.Error()
	}
	return execenv.NewRefusal(execenv.RefusalServiceUnhealthy, reason,
		"the loop's first command would have run against a dependency that is not answering, which fails later in "+
			"a way nobody can read; the service's container was created, the probe ran, and the probe said no (EV14)",
		"read the probe's sentence above, fix the service or its health check, and run the step again; what this "+
			"call created is being taken down")
}

// probeOnce is one attempt. `health.cmd` runs INSIDE the service container with
// the image's own tooling — a database image ships its own client, and asking it
// is more honest than guessing a protocol. `health.tcp` runs a separate probe
// container on the loop's network, so the check never needs a published port.
func (p *Provisioner) probeOnce(ctx context.Context, svc execenv.Service, row store.Provision, network, workDir string) (bool, string) {
	if h := svc.Health; h != nil && h.Command != "" {
		cmd, err := p.Runtime.Exec(ctx, ExecSpec{
			Handle: row.Handle, Command: h.Command, WorkDir: "/", Dir: workDir, User: p.User,
		})
		if err != nil {
			return false, err.Error()
		}
		res, ref := p.Probe(ctx, cmd, h.Command)
		if ref != nil {
			return false, ref.Sentence()
		}
		if res.ExitCode != 0 || res.TimedOut {
			return false, firstLine(res.Stderr, "exit "+intString(res.ExitCode))
		}
		return true, ""
	}
	cmd, err := p.Runtime.ProbeRun(ctx, ProbeSpec{
		LoopSlug: row.LoopSlug, Network: network, Image: svc.Image, User: p.User,
		Host: svc.Name, Port: healthPort(svc),
	})
	if err != nil {
		return false, err.Error()
	}
	command := probeConnect(svc.Name, healthPort(svc))
	res, ref := p.Probe(ctx, cmd, command)
	if ref != nil {
		return false, ref.Sentence()
	}
	if res.ExitCode != 0 || res.TimedOut {
		return false, firstLine(res.Stderr, "exit "+intString(res.ExitCode))
	}
	return true, ""
}

// healthPort is what the tcp probe dials: the declared health.tcp, falling back
// to the service's declared port, which is the port the app will use anyway.
func healthPort(svc execenv.Service) int {
	if svc.Health != nil && svc.Health.TCP > 0 {
		return svc.Health.TCP
	}
	return svc.Port
}

func orText(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func firstLine(text, def string) string {
	if text == "" {
		return def
	}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			return text[:i]
		}
	}
	return text
}
