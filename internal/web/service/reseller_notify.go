package service

import (
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"

	"gorm.io/gorm"
)

// bumpResellerPoolNotices updates notified_level and publishes reseller.pool after usage moves.
func bumpResellerPoolNotices(tx *gorm.DB, touched []int) error {
	if len(touched) == 0 {
		return nil
	}
	notifyOn, err := (&SettingService{}).GetResellerPoolNotify()
	if err != nil {
		notifyOn = false
	}
	for _, id := range touched {
		var r model.Reseller
		if err := tx.First(&r, id).Error; err != nil {
			return err
		}
		if r.PoolBytes <= 0 {
			continue
		}
		level := resellerPoolLevel(&r)
		if level <= r.NotifiedLevel {
			continue
		}
		if err := tx.Model(&model.Reseller{}).Where("id = ?", id).Update("notified_level", level).Error; err != nil {
			return err
		}
		if notifyOn {
			publishResellerPoolEvent(eventbus.Event{
				Type:   eventbus.EventResellerPool,
				Source: r.Name,
				Data: &eventbus.ResellerPoolData{
					ResellerId: id, Name: r.Name,
					UsedBytes: r.UsedTraffic, PoolBytes: r.PoolBytes, Level: level,
					Kind: "threshold",
				},
				Timestamp: time.Now(),
			})
		}
	}
	return nil
}

func resellerPoolLevel(r *model.Reseller) int {
	if r.PoolBytes <= 0 {
		return 0
	}
	if r.UsedTraffic >= r.PoolBytes {
		return 100
	}
	if r.UsedTraffic*100 >= r.PoolBytes*80 {
		return 80
	}
	return 0
}

func publishResellerCutoffNotice(r model.Reseller, reason string, clients int) {
	notifyOn, err := (&SettingService{}).GetResellerPoolNotify()
	if err != nil || !notifyOn {
		return
	}
	publishResellerPoolEvent(eventbus.Event{
		Type:   eventbus.EventResellerPool,
		Source: r.Name,
		Data: &eventbus.ResellerPoolData{
			ResellerId: r.Id, Name: r.Name,
			UsedBytes: r.UsedTraffic, PoolBytes: r.PoolBytes,
			Kind: "cutoff", Reason: reason, Clients: clients,
		},
		Timestamp: time.Now(),
	})
}

func publishResellerReleaseNotice(r model.Reseller, clients int) {
	notifyOn, err := (&SettingService{}).GetResellerPoolNotify()
	if err != nil || !notifyOn {
		return
	}
	publishResellerPoolEvent(eventbus.Event{
		Type:   eventbus.EventResellerPool,
		Source: r.Name,
		Data: &eventbus.ResellerPoolData{
			ResellerId: r.Id, Name: r.Name,
			UsedBytes: r.UsedTraffic, PoolBytes: r.PoolBytes,
			Kind: "release", Clients: clients,
		},
		Timestamp: time.Now(),
	})
}

func publishResellerPoolEvent(e eventbus.Event) {
	if eventBus != nil {
		eventBus.Publish(e)
	}
}