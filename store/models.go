// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.25.0

package store

import (
	"database/sql"
	"time"
)

type Player struct {
	SteamID          int64        `json:"steam_id"`
	Personaname      string       `json:"personaname"`
	Visibility       int64        `json:"visibility"`
	RealName         string       `json:"real_name"`
	AccountCreatedOn time.Time    `json:"account_created_on"`
	AvatarHash       string       `json:"avatar_hash"`
	CommunityBanned  bool         `json:"community_banned"`
	GameBans         int64        `json:"game_bans"`
	VacBans          int64        `json:"vac_bans"`
	LastVacBanOn     sql.NullTime `json:"last_vac_ban_on"`
	KillsOn          int64        `json:"kills_on"`
	DeathsBy         int64        `json:"deaths_by"`
	RageQuits        int64        `json:"rage_quits"`
	Notes            string       `json:"notes"`
	Whitelist        bool         `json:"whitelist"`
	ProfileUpdatedOn time.Time    `json:"profile_updated_on"`
	CreatedOn        time.Time    `json:"created_on"`
	UpdatedOn        time.Time    `json:"updated_on"`
}

type PlayerMessage struct {
	MessageID int64     `json:"message_id"`
	SteamID   int64     `json:"steam_id"`
	Message   string    `json:"message"`
	Team      bool      `json:"team"`
	CreatedOn time.Time `json:"created_on"`
}

type PlayerName struct {
	NameID    int64     `json:"name_id"`
	SteamID   int64     `json:"steam_id"`
	Name      string    `json:"name"`
	CreatedOn time.Time `json:"created_on"`
}
