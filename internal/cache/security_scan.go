package cache

import (
	"context"

	"go.uber.org/zap"
)

// SecurityScanner is the optional contract for scanning new packages on miss.
type SecurityScanner interface {
	ScanPackage(ctx context.Context, ecosystem, packageName string) error
}

type packageScan struct {
	ecosystem   string
	packageName string
	key         string
}

// SetSecurityScanner attaches an optional security scanner. Pass nil to detach.
func (m *Manager) SetSecurityScanner(s SecurityScanner) {
	m.componentMu.Lock()
	defer m.componentMu.Unlock()
	m.securityScanner = s
}

func (m *Manager) enqueueScan(ecosystem, packageName string) bool {
	if packageName == "" || m.scanner() == nil {
		return false
	}
	key := ecosystem + "\x00" + packageName
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.lifecycleClosed {
		return false
	}
	m.scanMu.Lock()
	defer m.scanMu.Unlock()
	if _, exists := m.scanPending[key]; exists {
		return false
	}
	job := packageScan{ecosystem: ecosystem, packageName: packageName, key: key}
	m.scanPending[key] = struct{}{}
	select {
	case m.scanQueue <- job:
		return true
	default:
		delete(m.scanPending, key)
		return false
	}
}

func (m *Manager) finishScan(job packageScan) {
	m.scanMu.Lock()
	delete(m.scanPending, job.key)
	m.scanMu.Unlock()
}

func (m *Manager) processScan(job packageScan) {
	defer m.finishScan(job)
	ctx, finishOwned, admitted := m.beginOwned()
	if !admitted {
		return
	}
	defer finishOwned()
	scanner := m.scanner()
	if scanner == nil {
		return
	}
	if err := scanner.ScanPackage(ctx, job.ecosystem, job.packageName); err != nil {
		zap.L().Debug("security scan for new package failed", zap.Error(err))
	}
}

func (m *Manager) runScanWorker(ctx context.Context) {
	for {
		select {
		case job := <-m.scanQueue:
			m.processScan(job)
		case <-ctx.Done():
			// Queued jobs have not started and are intentionally discarded at
			// shutdown. Active calls receive the cancelled lifecycle context and
			// remain covered by lifecycleWG.
			for {
				select {
				case job := <-m.scanQueue:
					m.finishScan(job)
				default:
					return
				}
			}
		}
	}
}

func (m *Manager) scanner() SecurityScanner {
	m.componentMu.RLock()
	defer m.componentMu.RUnlock()
	return m.securityScanner
}
