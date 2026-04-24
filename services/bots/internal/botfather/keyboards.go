// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botfather

import (
	"encoding/json"
	"fmt"

	"github.com/mst-corp/orbit/services/bots/internal/client"
	"github.com/mst-corp/orbit/services/bots/internal/model"
)

// InlineKeyboardButton matches the Telegram-compatible inline keyboard format.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

// InlineKeyboardMarkup is a grid of inline buttons.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

func marshalKeyboard(kb *InlineKeyboardMarkup) json.RawMessage {
	data, _ := json.Marshal(kb)
	return data
}

// buildBotListKeyboard creates an inline keyboard with one button per bot.
func buildBotListKeyboard(bots []model.Bot, callbackPrefix string) *InlineKeyboardMarkup {
	var rows [][]InlineKeyboardButton
	for _, b := range bots {
		rows = append(rows, []InlineKeyboardButton{
			{Text: fmt.Sprintf("@%s", b.Username), CallbackData: fmt.Sprintf("%s:%s", callbackPrefix, b.ID)},
		})
	}
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// buildManagementMenu creates the management menu for a selected bot.
func buildManagementMenu(botID string) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "Имя", CallbackData: fmt.Sprintf("manage:setname:%s", botID)},
				{Text: "Описание", CallbackData: fmt.Sprintf("manage:setdesc:%s", botID)},
			},
			{
				{Text: "Команды", CallbackData: fmt.Sprintf("manage:setcmds:%s", botID)},
				{Text: "Вебхук", CallbackData: fmt.Sprintf("manage:setwebhook:%s", botID)},
			},
			{
				{Text: "Токен", CallbackData: fmt.Sprintf("manage:token:%s", botID)},
				{Text: "Интеграция", CallbackData: fmt.Sprintf("manage:integration:%s", botID)},
			},
			{
				{Text: "Удалить", CallbackData: fmt.Sprintf("manage:delete:%s", botID)},
			},
			{
				{Text: "← Назад к списку", CallbackData: "manage:back"},
			},
		},
	}
}

// buildTokenActionsKeyboard creates actions for the /token command.
func buildTokenActionsKeyboard(botID string) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "Показать префикс", CallbackData: fmt.Sprintf("token:show:%s", botID)},
				{Text: "Перегенерировать", CallbackData: fmt.Sprintf("token:rotate:%s", botID)},
			},
		},
	}
}

// buildConnectorListKeyboard creates an inline keyboard with one button per connector.
func buildConnectorListKeyboard(connectors []client.ConnectorInfo, callbackPrefix string) *InlineKeyboardMarkup {
	var rows [][]InlineKeyboardButton
	for _, c := range connectors {
		label := c.DisplayName
		if label == "" {
			label = c.Name
		}
		rows = append(rows, []InlineKeyboardButton{
			{Text: label, CallbackData: fmt.Sprintf("%s:%s", callbackPrefix, c.ID)},
		})
	}
	// Add "clear" option
	rows = append(rows, []InlineKeyboardButton{
		{Text: "Снять привязку", CallbackData: fmt.Sprintf("%s:clear", callbackPrefix)},
	})
	return &InlineKeyboardMarkup{InlineKeyboard: rows}
}

// buildConfirmKeyboard creates a Yes/Cancel confirmation.
func buildConfirmKeyboard(yesData, cancelData string) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "Да, подтверждаю", CallbackData: yesData},
				{Text: "Отмена", CallbackData: cancelData},
			},
		},
	}
}

// buildToggleKeyboard creates an on/off toggle keyboard with localized labels.
// onData / offData become the callback_data for the two options.
func buildToggleKeyboard(onLabel, offLabel, onData, offData string) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: onLabel, CallbackData: onData},
				{Text: offLabel, CallbackData: offData},
			},
		},
	}
}

// buildMenuButtonTypeKeyboard creates the 4-row type selector for /setmenubutton.
func buildMenuButtonTypeKeyboard(botID string) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "default", CallbackData: fmt.Sprintf("setmenu:default:%s", botID)}},
			{{Text: "commands", CallbackData: fmt.Sprintf("setmenu:commands:%s", botID)}},
			{{Text: "web_app", CallbackData: fmt.Sprintf("setmenu:webapp:%s", botID)}},
			{{Text: "Сбросить", CallbackData: fmt.Sprintf("setmenu:clear:%s", botID)}},
		},
	}
}
