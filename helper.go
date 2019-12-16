package main

import (
	"bytes"
	"strings"
	"text/template"

	alerttemplate "github.com/prometheus/alertmanager/template"
	"github.com/spf13/viper"
)

func sendMessageToChat(alerts []alerttemplate.Alert, notif *Notifier, webHookURL string) error {
	var (
		message = ChatNotification{}
		str     strings.Builder
	)
	// common funcs used in template
	templateFuncMap := template.FuncMap{
		"Title":   strings.Title,
		"toUpper": strings.ToUpper,
	}
	// read template file
	tmpl, err := template.New("message.tmpl").Funcs(templateFuncMap).ParseFiles(viper.GetString("app.template_file"))
	if err != nil {
		errLog.Printf("Error reading template %s", err)
		return err
	}
	// loop through list of alerts and append the data in template
	size := len(alerts)
	for index, a := range alerts {

		sysLog.Printf("Processing alerts #%d", index)

		var to bytes.Buffer
		err = tmpl.Execute(&to, a)
		if err != nil {
			errLog.Printf("Error parsing values in template %s", err)
			return err
		}
		str.WriteString(to.String())
		str.WriteString("\n")
		if (((index + 1) % 7) == 0) || ((size - index) == 1) {
			// send message every 7 alert at maximum
			// message should not longer than 4096 characters since google chat limitation
			message.Text = str.String()
			if len(message.Text) > 7 {
				err := notif.PushNotification(message, webHookURL)
				if err != nil {
					errLog.Printf("Error pushing alerts #%d", index)
					errLog.Printf("Alerts %s", to.String())
					return err
				}
			}
			str.Reset()
		}
	}

	return nil
}
