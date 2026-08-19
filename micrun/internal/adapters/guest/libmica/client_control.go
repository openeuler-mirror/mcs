package libmica

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"micrun/internal/adapters/hypervisor/pedestal"
	"micrun/internal/support/contextx"
	defs "micrun/internal/support/definitions"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
)

func ensureMicadListening() error {
	if !validSocketPath(defs.MicaCreateSocketPath) {
		return er.MicadNotRunning
	}
	return nil
}

func validateClientID(id string) error {
	if id == "" {
		return fmt.Errorf("empty client id is not allowed")
	}
	// Reject path separators and NUL: the id is used in filepath.Join to
	// build the per-client control socket path. A tampered state file
	// carrying an id like "../../tmp/x" would redirect the path.
	if strings.ContainsAny(id, `/\`) || strings.ContainsRune(id, '\x00') {
		return fmt.Errorf("client id %q contains invalid path characters", id)
	}
	// Reject ids that micad would truncate: the daemon forces
	// name[MAX_NAME_LEN-1] = '\0' and derives the per-client control socket
	// path from the truncated name, so a full-length id would make every
	// subsequent control operation fail with ENOENT.
	if len(id) >= MaxNameLen {
		return fmt.Errorf("client id %q exceeds mica limit (%d characters)", id, MaxNameLen-1)
	}
	// Whitespace breaks micad status column parsing (strings.Fields on the
	// daemon side) and a leading dash makes xl subcommands parse the id as
	// an option — same tampered-state threat model as the path separators.
	if strings.ContainsFunc(id, unicode.IsSpace) || strings.HasPrefix(id, "-") {
		return fmt.Errorf("client id %q contains whitespace or a leading dash", id)
	}
	return nil
}

func clientSocketPath(id string) string {
	return filepath.Join(defs.MicaStateDir, id+".socket")
}

// Create creates a new mica client.
// Use the context-aware control functions to manage the mica client lifecycle.
//
// Deprecated: use CreateContext so callers can propagate cancellation.
func Create(config MicaClientConf) error {
	return CreateContext(context.Background(), config)
}

func CreateContext(ctx context.Context, config MicaClientConf) error {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	// Same length rule as every control operation: micad truncates
	// name[MAX_NAME_LEN-1] = '\0', so a full-length name would be
	// registered as 65 chars while all later control sockets use the
	// 66-char id — an uncontrollable, leaking client.
	if err := validateClientID(strings.TrimRight(string(config.name[:]), "\x00")); err != nil {
		return err
	}
	s := newMicaSocket(defs.MicaCreateSocketPath)
	// create is synchronous on the daemon side (firmware loading, domain
	// creation) and can take well beyond the default control timeout; give
	// it the long budget so a slow-but-healthy daemon is not misreported as
	// timed out (and retried while still executing).
	s.timeout = defs.MicaSocketLongTimeout
	return s.handleMsg(ctx, config.pack())
}

func micaCtlImpl(ctx context.Context, cmd MicaCommand, id string, opts ...string) error {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureMicadListening(); err != nil {
		return err
	}

	if err := validateClientID(id); err != nil {
		return err
	}

	msg, err := micaCommandMessage(cmd, opts)
	if err != nil {
		return err
	}

	s := newMicaSocket(clientSocketPath(id))
	// create/start/stop/rm are synchronous on the daemon side and can take
	// well beyond the default control timeout (firmware load, domain
	// lifecycle, remoteproc shutdown); give them a longer budget so a
	// slow-but-healthy daemon is not misreported as timed out (and retried
	// while still executing). Match on the wire command: MResume maps to
	// MStart (full domain start) and MPause maps to MStop (remoteproc
	// shutdown), so they are equally heavy and need the same budget.
	switch micaWireCommand(cmd) {
	case MCreate, MStart, MStop, MRemove:
		s.timeout = defs.MicaSocketLongTimeout
	}
	return s.handleMsg(ctx, []byte(msg))
}

func micaCommandMessage(cmd MicaCommand, opts []string) (string, error) {
	wireCmd := micaWireCommand(cmd)
	msg := string(wireCmd)
	if wireCmd != MUpdate {
		return msg, nil
	}

	update, err := buildUpdateWireFormat(opts)
	if err != nil {
		return "", err
	}
	return msg + " " + update, nil
}

func micaWireCommand(cmd MicaCommand) MicaCommand {
	switch cmd {
	case MPause:
		return MStop
	case MResume:
		return MStart
	default:
		return cmd
	}
}

func buildUpdateWireFormat(opts []string) (string, error) {
	req, err := parseMicaUpdateRequest(opts)
	if err != nil {
		return "", err
	}
	return req.WireFormat(), nil
}

func parseMicaUpdateRequest(opts []string) (MicaUpdateRequest, error) {
	resourceType, value := parseUpdateArgs(opts)
	if resourceType == "" || value == "" {
		return MicaUpdateRequest{}, fmt.Errorf("invalid update parameters: %v", opts)
	}
	req := MicaUpdateRequest{Field: MicaUpdateField(resourceType), Value: value}
	if !req.Field.Valid() {
		return MicaUpdateRequest{}, fmt.Errorf("unsupported mica update field %s", resourceType)
	}
	return req, nil
}

func parseUpdateArgs(opts []string) (resourceType, value string) {
	if len(opts) == 0 {
		return "", ""
	}
	if len(opts) == 1 {
		parts := strings.Fields(opts[0])
		if len(parts) == 0 {
			return "", ""
		}
		resourceType = parts[0]
		if len(parts) > 1 {
			value = strings.Join(parts[1:], " ")
		}
		return resourceType, value
	}
	resourceType = opts[0]
	if len(opts) > 1 {
		value = strings.Join(opts[1:], " ")
	}
	return resourceType, value
}

var micaCtlFn micaCtlFunc = micaCtlImpl

func micaCtlContext(ctx context.Context, cmd MicaCommand, id string, opts ...string) error {
	return micaCtlWithHypervisor(ctx, nil, cmd, id, opts...)
}

func micaCtlWithHypervisor(ctx context.Context, h hypervisorControl, cmd MicaCommand, id string, opts ...string) error {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if cmd == MUpdate && defs.WorkaroundUpdate {
		log.Debug("calling xl to update resource for debug")
		if err := handleMicaUpdateWithXl(ctx, h, id, opts...); err != nil {
			log.Debugf("xl workaround not available: %v", err)
		} else {
			return nil
		}
	}
	return micaCtlFn(ctx, cmd, id, opts...)
}

// Start starts a mica client.
//
// Deprecated: use StartContext so callers can propagate cancellation.
func Start(id string) error {
	return StartContext(context.Background(), id)
}

// StartContext starts a mica client with caller-controlled cancellation.
func StartContext(ctx context.Context, id string) error {
	if err := micaCtlContext(ctx, MStart, id); err != nil {
		return fmt.Errorf("failed to start container %s: %w", id, err)
	}
	return nil
}

// Stop stops the mica client (RTOS guest) by removing it from XEN.
// After stop, the sandbox state should be set to STOPPED to allow restart.
//
// Deprecated: use StopContext so callers can propagate cancellation.
func Stop(id string) error {
	return StopContext(context.Background(), id)
}

func StopContext(ctx context.Context, id string) error {
	missing, err := ClientNotExistContext(ctx, id)
	if err != nil {
		return err
	}
	if missing {
		log.Infof("%s is already down, not need to stop it", id)
	} else if err := micaCtlContext(ctx, MRemove, id); err != nil {
		return fmt.Errorf("failed to stop mica client %s %w", id, err)
	}
	return nil
}

// Pause pauses a mica client.
//
// Deprecated: use PauseWithHypervisorContext so callers can propagate cancellation.
func Pause(id string) error {
	return PauseWithHypervisor(id, nil)
}

// PauseWithHypervisor pauses a mica client with an optional hypervisor control.
//
// Deprecated: use PauseWithHypervisorContext so callers can propagate cancellation.
func PauseWithHypervisor(id string, h hypervisorControl) error {
	return PauseWithHypervisorContext(context.Background(), id, h)
}

func PauseWithHypervisorContext(ctx context.Context, id string, h hypervisorControl) error {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if h != nil {
		if err := h.Pause(ctx, id); err == nil {
			return nil
		} else if !errors.Is(err, pedestal.ErrNotSupported) {
			return err
		}
	}
	if err := micaCtlContext(ctx, MPause, id); err != nil {
		return fmt.Errorf("failed to pause mica client %s %w", id, err)
	}
	return nil
}

// Resume resumes a mica client.
//
// Deprecated: use ResumeWithHypervisorContext so callers can propagate cancellation.
func Resume(id string) error {
	return ResumeWithHypervisor(id, nil)
}

// ResumeWithHypervisor resumes a mica client with an optional hypervisor control.
//
// Deprecated: use ResumeWithHypervisorContext so callers can propagate cancellation.
func ResumeWithHypervisor(id string, h hypervisorControl) error {
	return ResumeWithHypervisorContext(context.Background(), id, h)
}

func ResumeWithHypervisorContext(ctx context.Context, id string, h hypervisorControl) error {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if h != nil {
		if err := h.Resume(ctx, id); err == nil {
			return nil
		} else if !errors.Is(err, pedestal.ErrNotSupported) {
			return err
		}
	}
	if err := micaCtlContext(ctx, MResume, id); err != nil {
		return fmt.Errorf("failed to resume mica client %s %w", id, err)
	}
	return nil
}

// Remove removes a mica client.
//
// Deprecated: use RemoveContext so callers can propagate cancellation.
func Remove(id string) error {
	return RemoveContext(context.Background(), id)
}

// RemoveContext removes a mica client with caller-controlled cancellation.
func RemoveContext(ctx context.Context, id string) error {
	missing, err := ClientNotExistContext(ctx, id)
	if err != nil {
		return err
	}
	if missing {
		return nil
	}
	return micaCtlContext(ctx, MRemove, id)
}
