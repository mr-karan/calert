package google_chat

import (
	"path/filepath"
	"testing"

	alertmgrtmpl "github.com/prometheus/alertmanager/template"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestGoogleChatTemplate(t *testing.T) {

	opts := &GoogleChatOpts{
		Log:      logrus.New(),
		Endpoint: "http://",
		Room:     "qa",
		Template: "../../../static/message.tmpl",
		DryRun:   true,
	}

	chat, err := NewGoogleChat(*opts)
	if err != nil || chat == nil {
		t.Fatal(err)
	}

	alert := alertmgrtmpl.Alert{
		Status: "firing",
		Labels: alertmgrtmpl.KV(map[string]string{
			"severity": "high", "alertname": "TestAlert",
		}),
		Annotations: alertmgrtmpl.KV(map[string]string{
			"team": "qa", "dryrun": "true",
		}),
	}

	expectedMessage := "*(HIGH) TestAlert - Firing*\nDryrun: true\nTeam: qa\n\n"

	msgs, err := chat.prepareMessage(alert)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "message.tmpl", filepath.Base(chat.msgTmpl.Name()), "Message template name")
	assert.Equal(t, msgs[0].Text, expectedMessage)

}
