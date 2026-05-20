package dota

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSteamProviderGetRecentMatches(t *testing.T) {
	t.Parallel()

	const accountID int64 = 1010282450

	mux := http.NewServeMux()
	mux.HandleFunc("/IDOTA2Match_570/GetMatchHistory/v1/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "steam-key" {
			t.Fatalf("key = %q", r.URL.Query().Get("key"))
		}
		if r.URL.Query().Get("account_id") != "1010282450" {
			t.Fatalf("account_id = %q", r.URL.Query().Get("account_id"))
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"result": {
				"status": 1,
				"matches": [{
					"match_id": 123,
					"start_time": 1700000000,
					"lobby_type": 7,
					"players": [{"account_id": 1010282450, "player_slot": 128, "hero_id": 1}]
				}]
			}
		}`))
	})
	mux.HandleFunc("/IDOTA2Match_570/GetMatchDetails/v1/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("match_id") != "123" {
			t.Fatalf("match_id = %q", r.URL.Query().Get("match_id"))
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"result": {
				"match_id": 123,
				"start_time": 1700000000,
				"lobby_type": 7,
				"game_mode": 22,
				"radiant_win": false,
				"players": [{"account_id": 1010282450, "player_slot": 128, "hero_id": 1}]
			}
		}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	provider := NewSteamProvider(server.URL, "steam-key", 5)
	matches, err := provider.GetRecentMatches(context.Background(), accountID+steamID64Base)
	if err != nil {
		t.Fatalf("GetRecentMatches() error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	if matches[0].MatchID != 123 || matches[0].PlayerSlot != 128 || !matches[0].HasResult || !IsCompetitiveMatch(matches[0]) {
		t.Fatalf("match = %#v", matches[0])
	}
	if got := ResolvePlayerResult(matches[0].PlayerSlot, matches[0].RadiantWin); got != "win" {
		t.Fatalf("ResolvePlayerResult() = %q, want win", got)
	}
}

func TestSteamProviderKeepsHistoryMatchWhenDetailsFail(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/IDOTA2Match_570/GetMatchHistory/v1/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"result": {
				"status": 1,
				"matches": [{
					"match_id": 123,
					"start_time": 1700000000,
					"lobby_type": 7,
					"players": [{"account_id": 1010282450, "player_slot": 0, "hero_id": 1}]
				}]
			}
		}`))
	})
	mux.HandleFunc("/IDOTA2Match_570/GetMatchDetails/v1/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "steam hiccup", http.StatusInternalServerError)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	provider := NewSteamProvider(server.URL, "steam-key", 5)
	matches, err := provider.GetRecentMatches(context.Background(), 1010282450)
	if err != nil {
		t.Fatalf("GetRecentMatches() error = %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	if matches[0].MatchID != 123 || matches[0].HasResult {
		t.Fatalf("match = %#v, want match without result", matches[0])
	}
	if !IsCompetitiveMatch(matches[0]) {
		t.Fatalf("IsCompetitiveMatch() = false, want true")
	}
}

func TestSteamProviderPrivateHistory(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":{"status":15,"statusDetail":"private profile"}}`))
	}))
	defer server.Close()

	provider := NewSteamProvider(server.URL, "steam-key", 5)
	_, err := provider.GetRecentMatches(context.Background(), 1010282450)
	if !errors.Is(err, ErrMatchHistoryPrivate) {
		t.Fatalf("GetRecentMatches() error = %v, want %v", err, ErrMatchHistoryPrivate)
	}
}
