package burst

import (
	"context"
	"fmt"
	"sort"
	"time"

	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/xtls/xray-core/app/observatory"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/signal/done"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/extension"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/features/routing"
	"google.golang.org/protobuf/proto"
)

type Observer struct {
	config *Config
	ctx    context.Context

	statusLock sync.Mutex
	hp         *HealthPing

	finished *done.Instance

	ohm outbound.Manager
}

func (o *Observer) GetObservation(ctx context.Context) (proto.Message, error) {
	return &observatory.ObservationResult{Status: o.createResult()}, nil
}

func (o *Observer) createResult() []*observatory.OutboundStatus {
	var result []*observatory.OutboundStatus
	o.hp.access.Lock()
	defer o.hp.access.Unlock()
	for name, value := range o.hp.Results {
		status := observatory.OutboundStatus{
			Alive:           value.getStatistics().All != value.getStatistics().Fail,
			Delay:           value.getStatistics().Average.Milliseconds(),
			LastErrorReason: "",
			OutboundTag:     name,
			LastSeenTime:    0,
			LastTryTime:     0,
			HealthPing: &observatory.HealthPingMeasurementResult{
				All:       int64(value.getStatistics().All),
				Fail:      int64(value.getStatistics().Fail),
				Deviation: int64(value.getStatistics().Deviation),
				Average:   int64(value.getStatistics().Average),
				Max:       int64(value.getStatistics().Max),
				Min:       int64(value.getStatistics().Min),
			},
		}
		result = append(result, &status)
	}
	return result
}

func (o *Observer) Type() interface{} {
	return extension.ObservatoryType()
}

func (o *Observer) Start() error {
	if o.config != nil && len(o.config.SubjectSelector) != 0 {
		o.finished = done.New()
		o.hp.StartScheduler(func() ([]string, error) {
			hs, ok := o.ohm.(outbound.HandlerSelector)
			if !ok {

				return nil, errors.New("outbound.Manager is not a HandlerSelector")
			}

			outbounds := hs.Select(o.config.SubjectSelector)
			return outbounds, nil
		})
		go o.background()
	}
	return nil
}

func (o *Observer) background() {
	for !o.finished.Done() {
		sleepTime := time.Second * 10
		if o.hp.Settings.Interval != 0 {
			sleepTime = o.hp.Settings.Interval * time.Duration(o.hp.Settings.SamplingCount)
		}

		// Visual Logging
		o.logVisualStatus()

		select {
		case <-time.After(sleepTime):
		case <-o.finished.Wait():
			return
		}
	}
}

func (o *Observer) logVisualStatus() {
	o.statusLock.Lock()
	defer o.statusLock.Unlock()

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(0, 1)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Bold(true).
		MarginBottom(1)

	rowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("46")).
		Bold(true)

	deadStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("160")).
		Faint(true)

	var rows []string
	rows = append(rows, titleStyle.Render("🔭 Observatory Status (Burst)"))

	statusList := o.createResult()

	// Find best
	var bestIdx = -1
	var minDelay int64 = 99999999

	for i, status := range statusList {
		if status.Alive && status.Delay < minDelay {
			minDelay = status.Delay
			bestIdx = i
		}
	}

	// Sort for consistent display
	sort.Slice(statusList, func(i, j int) bool {
		return statusList[i].OutboundTag < statusList[j].OutboundTag
	})

	// Re-calculate best index after sort is tricky, better to just highlight based on value or not rely on index.
	// Actually, standard observatory sorts outbounds names first, then probes.
	// Here we get result map, so order is random. We should sort first.

	// Let's re-do properly: sort first, then find best.

	// Sort
	sort.Slice(statusList, func(i, j int) bool {
		return statusList[i].OutboundTag < statusList[j].OutboundTag
	})

	// Find best again after sort
	bestIdx = -1
	minDelay = 99999999
	for i, status := range statusList {
		if status.Alive && status.Delay < minDelay {
			minDelay = status.Delay
			bestIdx = i
		}
	}

	for i, status := range statusList {
		icon := "  "
		style := rowStyle
		latency := fmt.Sprintf("%d ms", status.Delay)

		if !status.Alive {
			icon = "💀"
			style = deadStyle
			latency = "DEAD"
		} else if i == bestIdx {
			icon = "✅"
			style = selectedStyle
		}

		row := fmt.Sprintf("%s %-20s | %s", icon, status.OutboundTag, latency)
		rows = append(rows, style.Render(row))
	}

	if len(statusList) > 0 {
		fmt.Println(boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left, rows...)))
	}
}

func (o *Observer) Close() error {
	if o.finished != nil {
		o.hp.StopScheduler()
		return o.finished.Close()
	}
	return nil
}

func New(ctx context.Context, config *Config) (*Observer, error) {
	var outboundManager outbound.Manager
	var dispatcher routing.Dispatcher
	err := core.RequireFeatures(ctx, func(om outbound.Manager, rd routing.Dispatcher) {
		outboundManager = om
		dispatcher = rd
	})
	if err != nil {
		return nil, errors.New("Cannot get depended features").Base(err)
	}
	hp := NewHealthPing(ctx, dispatcher, config.PingConfig)
	return &Observer{
		config: config,
		ctx:    ctx,
		ohm:    outboundManager,
		hp:     hp,
	}, nil
}

func init() {
	common.Must(common.RegisterConfig((*Config)(nil), func(ctx context.Context, config interface{}) (interface{}, error) {
		return New(ctx, config.(*Config))
	}))
}
