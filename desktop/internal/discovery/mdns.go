package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	serviceType   = "_crosshare._tcp"
	serviceDomain = "local."
)

// PhoneInfo represents a discovered phone on the network.
type PhoneInfo struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	IP         net.IP `json:"ip"`
	Port       int    `json:"port"`
	APIVersion string `json:"api_version"`
	LastSeen   time.Time
}

// PhoneMediaItem represents a photo or video available on a phone.
type PhoneMediaItem struct {
	ContentHash string `json:"content_hash"`
	FileName    string `json:"file_name"`
	FileSize    int64  `json:"file_size"`
	MediaType   string `json:"media_type"` // "photo" or "video"
	DateTaken   string `json:"date_taken"` // RFC3339
	MimeType    string `json:"mime_type"`
}

// PhoneMediaList is the response from the phone's media list endpoint.
type PhoneMediaList struct {
	DeviceID   string           `json:"device_id"`
	DeviceName string           `json:"device_name"`
	Items      []PhoneMediaItem `json:"items"`
	TotalCount int              `json:"total_count"`
}

// Discovery manages mDNS-based discovery of phones on the local network.
type Discovery struct {
	interval     time.Duration
	phones       map[string]*PhoneInfo
	mu           sync.RWMutex
	onPhoneFound func(phone *PhoneInfo)
	cancel       context.CancelFunc
}

// New creates a new Discovery instance.
func New(interval time.Duration) *Discovery {
	return &Discovery{
		interval: interval,
		phones:   make(map[string]*PhoneInfo),
	}
}

// OnPhoneFound sets the callback invoked when a phone is discovered.
func (d *Discovery) OnPhoneFound(fn func(phone *PhoneInfo)) {
	d.onPhoneFound = fn
}

// Start begins periodic discovery scanning.
func (d *Discovery) Start(ctx context.Context) {
	ctx, d.cancel = context.WithCancel(ctx)

	// Do an immediate scan
	go d.scan(ctx)

	// Then scan periodically
	ticker := time.NewTicker(d.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.scan(ctx)
			}
		}
	}()
}

// ScanNow performs one discovery scan immediately.
func (d *Discovery) ScanNow(ctx context.Context) {
	d.scan(ctx)
}

// Stop stops the discovery scanner.
func (d *Discovery) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
}

// GetPhones returns all currently known phones.
func (d *Discovery) GetPhones() []*PhoneInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	phones := make([]*PhoneInfo, 0, len(d.phones))
	for _, p := range d.phones {
		phones = append(phones, p)
	}
	return phones
}

// scan performs a single mDNS discovery scan for phones.
func (d *Discovery) scan(ctx context.Context) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Printf("[discovery] failed to create resolver: %v", err)
		return
	}

	entries := make(chan *zeroconf.ServiceEntry, 16)

	// Scan for 10 seconds
	scanCtx, scanCancel := context.WithTimeout(ctx, 10*time.Second)
	defer scanCancel()

	go func() {
		for entry := range entries {
			d.handleEntry(entry)
		}
	}()

	if err := resolver.Browse(scanCtx, serviceType, serviceDomain, entries); err != nil {
		log.Printf("[discovery] browse failed: %v", err)
	}

	<-scanCtx.Done()
}

// handleEntry processes a discovered mDNS service entry.
func (d *Discovery) handleEntry(entry *zeroconf.ServiceEntry) {
	var ip net.IP
	if len(entry.AddrIPv4) > 0 {
		ip = entry.AddrIPv4[0]
	} else if len(entry.AddrIPv6) > 0 {
		ip = entry.AddrIPv6[0]
	} else {
		return
	}

	// Parse TXT records for metadata
	var deviceID, deviceName, apiVersion string
	for _, txt := range entry.Text {
		switch {
		case len(txt) > 3 && txt[:3] == "id=":
			deviceID = txt[3:]
		case len(txt) > 5 && txt[:5] == "name=":
			deviceName = txt[5:]
		case len(txt) > 4 && txt[:4] == "api=":
			apiVersion = txt[4:]
		}
	}

	if deviceID == "" {
		deviceID = entry.ServiceInstanceName()
	}
	if deviceName == "" {
		deviceName = entry.ServiceInstanceName()
	}

	phone := &PhoneInfo{
		DeviceID:   deviceID,
		DeviceName: deviceName,
		IP:         ip,
		Port:       entry.Port,
		APIVersion: apiVersion,
		LastSeen:   time.Now(),
	}

	d.mu.Lock()
	d.phones[deviceID] = phone
	d.mu.Unlock()

	log.Printf("[discovery] found phone: %s (%s) at %s:%d", deviceName, deviceID, ip, entry.Port)

	if d.onPhoneFound != nil {
		d.onPhoneFound(phone)
	}
}

// FetchMediaList fetches the list of available media from a phone.
func FetchMediaList(phone *PhoneInfo, token string) (*PhoneMediaList, error) {
	url := fmt.Sprintf("http://%s:%d/api/media/list", phone.IP, phone.Port)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch media list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("phone returned status %d: %s", resp.StatusCode, string(body))
	}

	var list PhoneMediaList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("failed to decode media list: %w", err)
	}

	return &list, nil
}

// DownloadMedia downloads a specific media file from the phone.
// It supports resuming from a given offset using HTTP Range headers.
// Returns the response body (caller must close) and the total file size.
func DownloadMedia(phone *PhoneInfo, contentHash string, token string, offset int64) (io.ReadCloser, int64, error) {
	url := fmt.Sprintf("http://%s:%d/api/media/download/%s", phone.IP, phone.Port, contentHash)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	client := &http.Client{Timeout: 0} // No timeout for large file downloads
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to download media: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("phone returned status %d", resp.StatusCode)
	}

	totalSize := resp.ContentLength
	if offset > 0 && resp.StatusCode == http.StatusPartialContent {
		totalSize += offset
	}

	return resp.Body, totalSize, nil
}

// NotifyMediaSynced notifies the phone that a list of media items has been successfully downloaded.
func NotifyMediaSynced(phone *PhoneInfo, token string, hashes []string) error {
	url := fmt.Sprintf("http://%s:%d/api/media/synced", phone.IP, phone.Port)

	payload := map[string]interface{}{
		"hashes": hashes,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to notify sync: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("phone returned status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
