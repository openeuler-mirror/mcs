package pedestal

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"micrun/internal/support/contextx"
	log "micrun/internal/support/logger"
)

func xlDomID(ctx context.Context, clientID string) (int, error) {
	var stdout, stderr bytes.Buffer
	cmd := newXLContext(ctx, domid, clientID)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return 0, fmt.Errorf("xl domid %s: %s", clientID, msg)
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return 0, fmt.Errorf("xl domid %s returned empty output", clientID)
	}

	domid, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("xl domid %s invalid output %q: %w", clientID, out, err)
	}
	return domid, nil
}

func domainID(ctx context.Context, clientID string) (int, error) {
	if domid, err := xlDomID(ctx, clientID); err == nil {
		return domid, nil
	} else {
		log.Debugf("xl domid fallback failed for %s: %v", clientID, err)
	}
	return parseXLListForDomain(ctx, clientID)
}

func parseXLListForDomain(ctx context.Context, clientID string) (int, error) {
	var stdout, stderr bytes.Buffer
	cmd := newXLContext(ctx, vmlist)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return 0, fmt.Errorf("xl list failed: %s", msg)
	}

	scanner := bufio.NewScanner(strings.NewReader(stdout.String()))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		// Skip only the actual header row: a HasPrefix("Name") check also
		// skipped any domain whose id starts with "Name" (column-match, the
		// same fix xlListDomainState already has).
		if len(fields) >= 2 && fields[0] == "Name" && fields[1] == "ID" {
			continue
		}
		if len(fields) < 2 || fields[0] != clientID {
			continue
		}

		domid, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, fmt.Errorf("xl list returned invalid domid %q for %s: %w", fields[1], clientID, err)
		}
		return domid, nil
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("xl list parse error: %w", err)
	}

	return 0, fmt.Errorf("domain %s not found in xl list output", clientID)
}

const xenstorePathFmt = "/local/domain/%d/%s"

func xenStoreRead(ctx context.Context, name, item string) (string, error) {
	domID, err := domainID(ctx, name)
	if err != nil {
		return "", err
	}
	xenstoreKey := fmt.Sprintf(xenstorePathFmt, domID, item)
	return xenStoreReadRaw(ctx, xenstoreKey)
}

func xenStoreReadRaw(ctx context.Context, key string) (string, error) {
	var stdout, stderr bytes.Buffer
	ctx = contextx.OrBackground(ctx)
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		// Same default bound as xl commands: a wedged xenstored must not
		// block console/lifecycle paths forever.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, xlCommandTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "xenstore-read", key)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("xenstore-read %s: %s", key, msg)
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("xenstore-read %s returned empty output", key)
	}
	return out, nil
}

// xlListDomainState resolves the domain ID via domainID (xl domid with
// xl list fallback), then reads the state column from `xl list <domID>`.
// Despite the historical name it does NOT use xenstore-read.
func xlListDomainState(ctx context.Context, name string) (string, error) {
	domID, err := domainID(ctx, name)
	if err != nil {
		return "", err
	}

	var stdout, stderr bytes.Buffer
	cmd := newXLContext(ctx, vmlist, fmt.Sprintf("%d", domID))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Include stderr for the same reason as xlDestroy: a transiently
		// vanished domain must be recognizable to isMissingDomainError.
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("failed to check domain %s state: %s", name, msg)
	}

	lines := strings.Split(stdout.String(), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		// Match the domain by its numeric ID in the ID column (fields[1]),
		// not by substring — a substring test on the domID digit can match
		// unrelated rows or the header line.
		if len(fields) < 5 {
			continue
		}
		id, err := strconv.Atoi(fields[1])
		if err != nil || id != domID {
			continue
		}
		state := fields[4]
		if len(state) > 0 && state[0] == 'r' {
			return "running", nil
		}
		return state, nil
	}

	return "", fmt.Errorf("domain %s not found in xl list", name)
}
