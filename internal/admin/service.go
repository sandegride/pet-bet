package admin

import (
	"context"
	"fmt"
	"strconv"

	"stavki/internal/domain"
)

// Service управляет настройками администратора.
type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// GetSettings возвращает текущие настройки.
func (s *Service) GetSettings(ctx context.Context) (domain.AdminSettings, error) {
	return s.repo.GetSettings(ctx)
}

// SetDefaultOdds устанавливает коэффициент для ставки "победа".
func (s *Service) SetDefaultOdds(ctx context.Context, odds string) error {
	return s.repo.SetSetting(ctx, "default_odds", odds)
}

// SetKillsOverOdds устанавливает коэффициент для ставки "тотал килов".
func (s *Service) SetKillsOverOdds(ctx context.Context, odds string) error {
	return s.repo.SetSetting(ctx, "kills_over_odds", odds)
}

// SetFirstBloodOdds устанавливает коэффициент для ставки "первая кровь".
func (s *Service) SetFirstBloodOdds(ctx context.Context, odds string) error {
	return s.repo.SetSetting(ctx, "first_blood_odds", odds)
}

// ToggleSoloOnly переключает фильтр "только соло игры" и возвращает новое значение.
func (s *Service) ToggleSoloOnly(ctx context.Context) (bool, error) {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return false, err
	}
	newValue := !settings.SoloOnlyBets
	if err := s.repo.SetSetting(ctx, "solo_only_bets", fmt.Sprintf("%v", newValue)); err != nil {
		return false, err
	}
	return newValue, nil
}

// SetMinAvgMMR устанавливает минимальный средний рейтинг (0 = отключено).
func (s *Service) SetMinAvgMMR(ctx context.Context, mmr int) error {
	return s.repo.SetSetting(ctx, "min_avg_mmr", strconv.Itoa(mmr))
}

// SetCSDefaultOdds устанавливает коэффициент для ставки "победа" в CS2.
func (s *Service) SetCSDefaultOdds(ctx context.Context, odds string) error {
	return s.repo.SetSetting(ctx, "cs_default_odds", odds)
}

// SetCSKillsOverOdds устанавливает коэффициент для ставки "тотал килов" в CS2.
func (s *Service) SetCSKillsOverOdds(ctx context.Context, odds string) error {
	return s.repo.SetSetting(ctx, "cs_kills_over_odds", odds)
}

// ToggleHWIDRequired переключает требование привязки железа.
func (s *Service) ToggleHWIDRequired(ctx context.Context) (bool, error) {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return false, err
	}
	newValue := !settings.HWIDRequired
	if err := s.repo.SetSetting(ctx, "hwid_required", fmt.Sprintf("%v", newValue)); err != nil {
		return false, err
	}
	return newValue, nil
}
