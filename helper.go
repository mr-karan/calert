package main

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/spf13/viper"

	alerttemplate "github.com/prometheus/alertmanager/template"
)

func sendMessageToChat(alerts []alerttemplate.Alert, notif *Notifier) error {
	var message = ChatNotification{}
	var str strings.Builder
	templateFuncMap := template.FuncMap{
		"Title":   strings.Title,
		"toUpper": strings.ToUpper,
	}
	tmpl, err := template.New(viper.GetString("app.template_file")).Funcs(templateFuncMap).ParseFiles(viper.GetString("app.template_file"))
	if err != nil {
		errLog.Printf("Error reading template %s", err)
		return err
	}
	for _, a := range alerts {
		var to bytes.Buffer
		err = tmpl.Execute(&to, a)
		if err != nil {
			errLog.Printf("Error parsing values in template %s", err)
			return err
		}
		str.WriteString(to.String())
		str.WriteString("\n")
	}
	message.Text = str.String()

	return notif.PushNotification(message)
}
