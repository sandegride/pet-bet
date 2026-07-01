package cs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// FaceitProvider — реальный провайдер данных о матчах CS2 через FACEIT Data API v4.
// Документация: https://docs.faceit.com/docs/data-api/data-api
type FaceitProvider struct {
	baseURL string
	apiKey  string
	game    string
	client  *http.Client
}

func NewFaceitProvider(baseURL, apiKey string) *FaceitProvider {
	if baseURL == "" {
		baseURL = "https://open.faceit.com/data/v4"
	}
	return &FaceitProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  strings.TrimSpace(apiKey),
		game:    "cs2",
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *FaceitProvider) ResolvePlayer(ctx context.Context, input string) (Player, error) {
	if p.apiKey == "" {
		return Player{}, ErrFaceitAPIKeyRequired
	}

	normalized, err := ParseAccountInput(input)
	if err != nil {
		return Player{}, err
	}

	values := url.Values{}
	if IsSteamID64(normalized) {
		values.Set("game", p.game)
		values.Set("game_player_id", normalized)
	} else {
		values.Set("nickname", normalized)
	}

	var row struct {
		PlayerID  string `json:"player_id"`
		Nickname  string `json:"nickname"`
		SteamID64 string `json:"steam_id_64"`
	}

	if err := p.getJSON(ctx, "/players?"+values.Encode(), &row); err != nil {
		return Player{}, err
	}
	if row.PlayerID == "" {
		return Player{}, ErrPlayerNotFound
	}

	return Player{
		PlayerID: row.PlayerID,
		Nickname: row.Nickname,
		SteamID:  row.SteamID64,
	}, nil
}

func (p *FaceitProvider) GetRecentMatches(ctx context.Context, playerID string) ([]RecentMatch, error) {
	if p.apiKey == "" {
		return nil, ErrFaceitAPIKeyRequired
	}

	values := url.Values{}
	values.Set("game", p.game)
	values.Set("offset", "0")
	values.Set("limit", "20")

	var history struct {
		Items []struct {
			MatchID string `json:"match_id"`
			Status  string `json:"status"`
			Teams   struct {
				Faction1 struct {
					Players []struct {
						PlayerID string `json:"player_id"`
					} `json:"players"`
				} `json:"faction1"`
				Faction2 struct {
					Players []struct {
						PlayerID string `json:"player_id"`
					} `json:"players"`
				} `json:"faction2"`
			} `json:"teams"`
			Results struct {
				Winner string `json:"winner"`
			} `json:"results"`
			StartedAt  int64 `json:"started_at"`
			FinishedAt int64 `json:"finished_at"`
		} `json:"items"`
	}

	if err := p.getJSON(ctx, fmt.Sprintf("/players/%s/history?%s", url.PathEscape(playerID), values.Encode()), &history); err != nil {
		return nil, err
	}

	matches := make([]RecentMatch, 0, len(history.Items))
	for _, item := range history.Items {
		if item.Results.Winner == "" {
			// Матч ещё не завершён или результат недоступен.
			continue
		}

		faction := ""
		for _, player := range item.Teams.Faction1.Players {
			if player.PlayerID == playerID {
				faction = "faction1"
				break
			}
		}
		if faction == "" {
			for _, player := range item.Teams.Faction2.Players {
				if player.PlayerID == playerID {
					faction = "faction2"
					break
				}
			}
		}
		if faction == "" {
			// Игрок не найден в составах — пропускаем (например, ещё не завершённый матч).
			continue
		}

		startedAt := item.StartedAt
		if startedAt == 0 {
			startedAt = item.FinishedAt
		}

		matches = append(matches, RecentMatch{
			MatchID:   item.MatchID,
			StartedAt: time.Unix(startedAt, 0).UTC(),
			HasResult: true,
			Won:       faction == item.Results.Winner,
		})
	}

	return matches, nil
}

func (p *FaceitProvider) GetMatchDetails(ctx context.Context, matchID string) (*MatchDetails, error) {
	if p.apiKey == "" {
		return nil, ErrFaceitAPIKeyRequired
	}

	var stats struct {
		Rounds []struct {
			RoundStats struct {
				Map string `json:"Map"`
			} `json:"round_stats"`
			Teams []struct {
				Players []struct {
					PlayerStats struct {
						Kills string `json:"Kills"`
					} `json:"player_stats"`
				} `json:"players"`
			} `json:"teams"`
		} `json:"rounds"`
	}

	if err := p.getJSON(ctx, fmt.Sprintf("/matches/%s/stats", url.PathEscape(matchID)), &stats); err != nil {
		return nil, err
	}
	if len(stats.Rounds) == 0 {
		return nil, ErrMatchDetailsMissing
	}

	round := stats.Rounds[0]
	totalKills := 0
	for _, team := range round.Teams {
		for _, player := range team.Players {
			if kills, err := strconv.Atoi(strings.TrimSpace(player.PlayerStats.Kills)); err == nil {
				totalKills += kills
			}
		}
	}

	return &MatchDetails{
		MatchID:    matchID,
		Map:        round.RoundStats.Map,
		TotalKills: totalKills,
	}, nil
}

func (p *FaceitProvider) getJSON(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "application/json")

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		resp, err := p.client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
			continue
		}

		func() {
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound {
				lastErr = ErrPlayerNotFound
				return
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				lastErr = fmt.Errorf("%w: faceit %s returned status %d", ErrProviderUnavailable, path, resp.StatusCode)
				return
			}

			if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
				lastErr = fmt.Errorf("decode faceit response: %w", err)
				return
			}
			lastErr = nil
		}()

		if lastErr == nil {
			return nil
		}
		if lastErr == ErrPlayerNotFound {
			return lastErr
		}
	}

	return lastErr
}
