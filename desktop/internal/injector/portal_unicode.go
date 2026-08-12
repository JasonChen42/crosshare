package injector

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	portalDestination = "org.freedesktop.portal.Desktop"
	portalObjectPath  = dbus.ObjectPath("/org/freedesktop/portal/desktop")

	remoteDesktopInterface = "org.freedesktop.portal.RemoteDesktop"
	requestInterface       = "org.freedesktop.portal.Request"

	deviceKeyboard = uint32(1)

	keyPress   = uint32(1)
	keyRelease = uint32(0)

	xkReturn   = int32(0xff0d)
	xkControlL = int32(0xffe3)
	xkShiftL   = int32(0xffe1)
)

type PortalUnicode struct {
	conn     *dbus.Conn
	session  dbus.ObjectPath
	mu       sync.Mutex
	typingMu sync.Mutex
	delay    time.Duration
}

func NewPortalUnicode() (*PortalUnicode, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, fmt.Errorf("connect session bus: %w", err)
	}
	return &PortalUnicode{conn: conn, delay: 6 * time.Millisecond}, nil
}

func (p *PortalUnicode) Preauthorize(ctx context.Context) error {
	return p.ensureSession(ctx)
}

func (p *PortalUnicode) InjectText(ctx context.Context, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	p.typingMu.Lock()
	defer p.typingMu.Unlock()

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err := p.ensureSession(ctx); err != nil {
			return err
		}

		injectedRunes := 0
		for _, r := range text {
			if err := p.injectRune(ctx, r); err != nil {
				lastErr = err
				if injectedRunes == 0 && isInvalidSessionError(err) {
					p.resetSession()
					break
				}
				return err
			}
			injectedRunes++
		}
		if lastErr == nil || !isInvalidSessionError(lastErr) {
			return lastErr
		}
		lastErr = nil
	}
	return errors.New("portal remote desktop session is invalid")
}

func (p *PortalUnicode) ensureSession(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session != "" {
		return nil
	}

	session, err := p.createSession(ctx)
	if err != nil {
		return err
	}
	if err := p.selectKeyboard(ctx, session); err != nil {
		return err
	}
	if err := p.start(ctx, session); err != nil {
		return err
	}
	p.session = session
	return nil
}

func (p *PortalUnicode) resetSession() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.session = ""
}

func (p *PortalUnicode) createSession(ctx context.Context) (dbus.ObjectPath, error) {
	options := map[string]dbus.Variant{
		"session_handle_token": dbus.MakeVariant(token("crosshare_session")),
		"handle_token":         dbus.MakeVariant(token("crosshare_create")),
	}

	var handle dbus.ObjectPath
	if err := p.portal().CallWithContext(ctx, remoteDesktopInterface+".CreateSession", 0, options).Store(&handle); err != nil {
		return "", fmt.Errorf("create portal remote desktop session: %w", err)
	}

	results, err := p.waitForRequest(ctx, handle)
	if err != nil {
		return "", err
	}
	value, ok := results["session_handle"]
	if !ok {
		return "", errors.New("portal create session response missing session_handle")
	}
	session, err := objectPathVariant(value)
	if err != nil {
		return "", fmt.Errorf("portal session_handle: %w", err)
	}
	return session, nil
}

func (p *PortalUnicode) selectKeyboard(ctx context.Context, session dbus.ObjectPath) error {
	options := map[string]dbus.Variant{
		"types":        dbus.MakeVariant(deviceKeyboard),
		"handle_token": dbus.MakeVariant(token("crosshare_select")),
	}

	var handle dbus.ObjectPath
	if err := p.portal().CallWithContext(ctx, remoteDesktopInterface+".SelectDevices", 0, session, options).Store(&handle); err != nil {
		return fmt.Errorf("select portal keyboard device: %w", err)
	}
	_, err := p.waitForRequest(ctx, handle)
	return err
}

func (p *PortalUnicode) start(ctx context.Context, session dbus.ObjectPath) error {
	options := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(token("crosshare_start")),
	}

	var handle dbus.ObjectPath
	if err := p.portal().CallWithContext(ctx, remoteDesktopInterface+".Start", 0, session, "", options).Store(&handle); err != nil {
		return fmt.Errorf("start portal remote desktop session: %w", err)
	}
	_, err := p.waitForRequest(ctx, handle)
	return err
}

func (p *PortalUnicode) waitForRequest(ctx context.Context, handle dbus.ObjectPath) (map[string]dbus.Variant, error) {
	signals := make(chan *dbus.Signal, 8)
	p.conn.Signal(signals)
	defer p.conn.RemoveSignal(signals)

	if err := p.conn.AddMatchSignal(
		dbus.WithMatchObjectPath(handle),
		dbus.WithMatchInterface(requestInterface),
		dbus.WithMatchMember("Response"),
	); err != nil {
		return nil, fmt.Errorf("listen for portal response: %w", err)
	}
	defer p.conn.RemoveMatchSignal(
		dbus.WithMatchObjectPath(handle),
		dbus.WithMatchInterface(requestInterface),
		dbus.WithMatchMember("Response"),
	)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case signal := <-signals:
			if signal == nil || signal.Path != handle || len(signal.Body) != 2 {
				continue
			}
			response, ok := signal.Body[0].(uint32)
			if !ok {
				return nil, fmt.Errorf("portal response code has unexpected type %T", signal.Body[0])
			}
			results, ok := signal.Body[1].(map[string]dbus.Variant)
			if !ok {
				return nil, fmt.Errorf("portal response results have unexpected type %T", signal.Body[1])
			}
			switch response {
			case 0:
				return results, nil
			case 1:
				return nil, errors.New("portal request was cancelled")
			case 2:
				return nil, errors.New("portal request failed")
			default:
				return nil, fmt.Errorf("portal request returned response code %d", response)
			}
		}
	}
}

func (p *PortalUnicode) injectRune(ctx context.Context, r rune) error {
	if r == '\r' {
		return nil
	}
	if r == '\n' {
		return p.tapKeysym(ctx, xkReturn)
	}

	hex := fmt.Sprintf("%x", r)
	if err := p.key(ctx, xkControlL, keyPress); err != nil {
		return err
	}
	controlPressed := true
	defer func() {
		if controlPressed {
			_ = p.key(context.Background(), xkControlL, keyRelease)
		}
	}()
	if err := p.key(ctx, xkShiftL, keyPress); err != nil {
		return err
	}
	shiftPressed := true
	defer func() {
		if shiftPressed {
			_ = p.key(context.Background(), xkShiftL, keyRelease)
		}
	}()
	if err := p.tapKeysym(ctx, int32('u')); err != nil {
		return err
	}
	if err := p.key(ctx, xkShiftL, keyRelease); err != nil {
		return err
	}
	shiftPressed = false
	if err := p.key(ctx, xkControlL, keyRelease); err != nil {
		return err
	}
	controlPressed = false

	for _, digit := range hex {
		if err := p.tapKeysym(ctx, int32(digit)); err != nil {
			return err
		}
	}
	return p.tapKeysym(ctx, xkReturn)
}

func (p *PortalUnicode) tapKeysym(ctx context.Context, keysym int32) error {
	if err := p.key(ctx, keysym, keyPress); err != nil {
		return err
	}
	return p.key(ctx, keysym, keyRelease)
}

func (p *PortalUnicode) key(ctx context.Context, keysym int32, state uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	call := p.portal().CallWithContext(
		ctx,
		remoteDesktopInterface+".NotifyKeyboardKeysym",
		0,
		p.session,
		map[string]dbus.Variant{},
		keysym,
		state,
	)
	if call.Err != nil {
		return fmt.Errorf("notify portal keyboard keysym %#x: %w", keysym, call.Err)
	}
	if p.delay > 0 {
		timer := time.NewTimer(p.delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func (p *PortalUnicode) portal() dbus.BusObject {
	return p.conn.Object(portalDestination, portalObjectPath)
}

func objectPathVariant(value dbus.Variant) (dbus.ObjectPath, error) {
	switch path := value.Value().(type) {
	case dbus.ObjectPath:
		if path == "" {
			return "", errors.New("empty object path")
		}
		return path, nil
	case string:
		if !dbus.ObjectPath(path).IsValid() {
			return "", fmt.Errorf("invalid object path %q", path)
		}
		return dbus.ObjectPath(path), nil
	default:
		return "", fmt.Errorf("unexpected type %T", value.Value())
	}
}

func token(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), rand.Int63())
}

func isInvalidSessionError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "invalid session")
}
