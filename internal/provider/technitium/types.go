package technitium

import (
	"encoding/json"
	"strings"
)

// envelope is the standard Technitium API response wrapper.
type envelope struct {
	Status       string          `json:"status"` // "ok" / "error"
	ErrorMessage string          `json:"errorMessage"`
	Response     json.RawMessage `json:"response"`
}

// zonesList is the response of /api/zones/list.
type zonesList struct {
	Zones []zone `json:"zones"`
}

type zone struct {
	Name         string `json:"name"`
	Type         string `json:"type"` // Primary / Secondary / Forwarder / ...
	Disabled     bool   `json:"disabled"`
	DnssecStatus string `json:"dnssecStatus"`
}

// recordsGet is the response of /api/zones/records/get.
type recordsGet struct {
	Records []record `json:"records"`
}

type record struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	TTL      int    `json:"ttl"`
	Disabled bool   `json:"disabled"`
	RData    rdata  `json:"rData"`
}

// rdata is the polymorphic record data object; we keep the common fields and fall
// back to a compact rendering of whatever is present.
type rdata struct {
	IPAddress  string `json:"ipAddress,omitempty"`
	CName      string `json:"cname,omitempty"`
	NameServer string `json:"nameServer,omitempty"`
	PtrName    string `json:"ptrName,omitempty"`
	Text       string `json:"text,omitempty"`
	Exchange   string `json:"exchange,omitempty"`
	Mailbox    string `json:"mailbox,omitempty"`
}

// summary renders the record's data as a short single-line string for the table.
func (d rdata) summary() string {
	for _, v := range []string{d.IPAddress, d.CName, d.NameServer, d.PtrName, d.Text, d.Exchange, d.Mailbox} {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
