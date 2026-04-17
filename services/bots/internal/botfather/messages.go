package botfather

import "fmt"

// Response messages (Russian, matching Orbit's primary UI language)

const msgWelcome = `Привет! Я BotFather — помогу создать и настроить ботов для Orbit.

Доступные команды:
/newbot — создать нового бота
/mybots — управление моими ботами
/help — список всех команд`

const msgHelp = `Команды BotFather:

Создание и управление:
/newbot — создать нового бота
/mybots — список и управление моими ботами

Настройка бота:
/setname — изменить имя бота
/setdescription — изменить описание
/setabouttext — короткий текст about (120 символов)
/setcommands — задать список команд
/setprivacy — режим приватности в группах
/setinline — inline-режим
/setjoingroups — разрешить добавлять в группы
/setmenubutton — кнопка меню у композера
/setwebhook — настроить вебхук
/token — показать или перегенерировать токен
/revoke — полная ротация токена (hard reset)
/deletebot — удалить бота

Другое:
/cancel — отменить текущую операцию
/help — показать эту справку`

const msgCancelled = "Операция отменена."
const msgNothingToCancel = "Нет активной операции для отмены."

const msgUnknownCommand = "Неизвестная команда. Отправь /help для списка доступных команд."
const msgTextOnly = "Я понимаю только текстовые сообщения. Отправь /help для списка команд."

// /newbot
const msgNewBotAskName = "Отлично! Давай создадим нового бота.\n\nКак назовём бота? Отправь отображаемое имя."
const msgNewBotAskUsername = "Хорошо. Теперь придумай username для бота (латиница, цифры, подчёркивание, 3–32 символа)."
const msgNewBotInvalidUsername = "Username может содержать только латинские буквы, цифры и подчёркивание (3–32 символа). Попробуй ещё раз."
const msgNewBotUsernameTaken = "Username @%s уже занят. Попробуй другой."

func msgNewBotCreated(username, token string) string {
	return fmt.Sprintf("Готово! Бот @%s создан.\n\nТокен:\n`%s`\n\nСохрани его — повторно показать полный токен нельзя.\n\nИспользуй /mybots для управления ботом.", username, token)
}

const msgBotLimitReached = "Достигнут лимит ботов (%d). Удали неиспользуемых ботов через /deletebot."

// /mybots
const msgNoBots = "У тебя пока нет ботов. Используй /newbot для создания."

func msgBotSelected(bot string) string {
	return fmt.Sprintf("Бот @%s — выбери действие:", bot)
}

// /setname
const msgSetNameSelectBot = "Выбери бота для изменения имени:"
const msgSetNameAwait = "Отправь новое отображаемое имя для бота:"

func msgSetNameDone(username string) string {
	return fmt.Sprintf("Имя бота @%s обновлено.", username)
}

// /setdescription
const msgSetDescSelectBot = "Выбери бота для изменения описания:"
const msgSetDescAwait = "Отправь новое описание для бота (до 512 символов):"

func msgSetDescDone(username string) string {
	return fmt.Sprintf("Описание бота @%s обновлено.", username)
}

// /setwebhook
const msgSetWebhookSelectBot = "Выбери бота для настройки вебхука:"
const msgSetWebhookAwait = "Отправь URL вебхука (HTTPS) или 'clear' для удаления:"
const msgSetWebhookCleared = "Вебхук удалён."

func msgSetWebhookDone(username, url string) string {
	return fmt.Sprintf("Вебхук для @%s установлен:\n%s", username, url)
}

const msgSetWebhookInvalid = "URL должен начинаться с https://. Попробуй ещё раз."
const msgSetWebhookAskSecret = "Отправь секретный ключ для подписи webhook payload (HMAC-SHA256), или 'skip' чтобы пропустить:"

// /setcommands
const msgSetCmdsSelectBot = "Выбери бота для настройки команд:"
const msgSetCmdsAwait = "Отправь команды в формате (каждая на новой строке):\n\ncommand1 - Описание команды\ncommand2 - Описание команды"

func msgSetCmdsDone(username string, count int) string {
	return fmt.Sprintf("Команды бота @%s обновлены (%d шт.).", username, count)
}

const msgSetCmdsInvalid = "Неверный формат. Каждая строка должна быть в формате:\ncommand - Описание\n\nПопробуй ещё раз."

// /deletebot
const msgDeleteSelectBot = "Выбери бота для удаления:"
const msgDeleteConfirm = "Ты уверен, что хочешь удалить бота @%s? Это действие нельзя отменить."

func msgDeleteDone(username string) string {
	return fmt.Sprintf("Бот @%s удалён.", username)
}

const msgDeleteCancelled = "Удаление отменено."

// /token
const msgTokenSelectBot = "Выбери бота:"

func msgTokenPrefix(username, prefix string) string {
	return fmt.Sprintf("Токен бота @%s:\n`%s...`\n\nПолный токен показывается только при создании или ротации.", username, prefix)
}

func msgTokenRotated(username, token string) string {
	return fmt.Sprintf("Новый токен для @%s:\n`%s`\n\nСтарый токен больше не действует. Сохрани новый.", username, token)
}

const msgTokenConfirmRotate = "Перегенерировать токен? Старый токен перестанет работать."

// System bot protection
const msgSystemBotProtected = "Системные боты не могут быть изменены через BotFather."

// /setintegration
const msgIntegrationSelectBot = "Выбери бота для привязки к коннектору:"
const msgIntegrationSelectConnector = "Выбери коннектор для привязки:"
const msgIntegrationNoConnectors = "Нет доступных коннекторов. Создай коннектор через админку интеграций."

func msgIntegrationDone(botUsername, connectorName string) string {
	return fmt.Sprintf("Коннектор '%s' привязан к @%s.\nВходящие вебхуки теперь будут постить от имени бота.", connectorName, botUsername)
}

func msgIntegrationCleared(botUsername string) string {
	return fmt.Sprintf("Привязка коннектора к @%s снята.", botUsername)
}

const msgIntegrationNotAvailable = "Сервис интеграций недоступен. Попробуй позже."

// Errors
const msgBotNotFound = "Бот не найден или не принадлежит тебе."
const msgInternalError = "Произошла внутренняя ошибка. Попробуй позже."

// /setabouttext
const msgSetAboutSelectBot = "Выбери бота для изменения about:"
const msgSetAboutAwait = "Отправь краткий текст about (до 120 символов). Этот текст показывается на странице профиля бота. 'clear' — очистить."
const msgSetAboutTooLong = "About может быть до 120 символов. Попробуй ещё раз."

func msgSetAboutDone(username string) string {
	return fmt.Sprintf("About бота @%s обновлён.", username)
}

// /setprivacy
const msgSetPrivacySelectBot = "Выбери бота для настройки режима приватности:"
const msgSetPrivacyPrompt = `Режим приватности определяет, какие сообщения бот видит в группах.

• Включён: только команды (/foo), ответы на сообщения бота, и сообщения, начинающиеся с @botname.
• Выключен: все сообщения в группе.

Текущее состояние: %s

Выбери новое значение:`

func msgSetPrivacyDone(username string, enabled bool) string {
	if enabled {
		return fmt.Sprintf("Режим приватности для @%s включён. Бот видит только команды.", username)
	}
	return fmt.Sprintf("Режим приватности для @%s выключен. Бот видит все сообщения.", username)
}

// /setinline
const msgSetInlineSelectBot = "Выбери бота для настройки inline-режима:"
const msgSetInlinePrompt = `Inline-режим позволяет вызывать бота из любого чата через @botname query.

Текущее состояние: %s

Выбери новое значение:`
const msgSetInlineAskPlaceholder = "Отправь placeholder для inline-режима (до 64 символов), или 'skip' чтобы оставить пустым:"
const msgSetInlinePlaceholderTooLong = "Placeholder может быть до 64 символов. Попробуй ещё раз."

func msgSetInlineOn(username string) string {
	return fmt.Sprintf("Inline-режим для @%s включён.", username)
}

func msgSetInlineOff(username string) string {
	return fmt.Sprintf("Inline-режим для @%s выключен.", username)
}

// /setjoingroups
const msgSetJoinGroupsSelectBot = "Выбери бота для настройки добавления в группы:"
const msgSetJoinGroupsPrompt = `Настройка определяет, могут ли пользователи добавлять бота в групповые чаты.

Текущее состояние: %s

Выбери новое значение:`

func msgSetJoinGroupsDone(username string, allowed bool) string {
	if allowed {
		return fmt.Sprintf("Бота @%s теперь можно добавлять в группы.", username)
	}
	return fmt.Sprintf("Бота @%s больше нельзя добавлять в группы.", username)
}

// /setmenubutton
const msgSetMenuSelectBot = "Выбери бота для настройки кнопки меню:"
const msgSetMenuPrompt = `Кнопка меню отображается слева от поля ввода в чате с ботом.

• default — стандартная кнопка ☰ (список команд)
• commands — кнопка «Команды» (открывает список /commands)
• web_app — кнопка открывает Web App

Выбери тип кнопки:`
const msgSetMenuAskText = "Отправь текст кнопки (до 32 символов):"
const msgSetMenuAskURL = "Отправь URL Web App (начиная с https://):"
const msgSetMenuInvalidURL = "URL должен начинаться с https://. Попробуй ещё раз."
const msgSetMenuTextTooLong = "Текст кнопки может быть до 32 символов. Попробуй ещё раз."

func msgSetMenuDone(username, kind string) string {
	return fmt.Sprintf("Кнопка меню для @%s установлена: %s.", username, kind)
}

func msgSetMenuCleared(username string) string {
	return fmt.Sprintf("Кнопка меню для @%s сброшена на стандартную.", username)
}

// /revoke
const msgRevokeSelectBot = "Выбери бота для полной ротации токена:"
const msgRevokeConfirm = `Полная ротация токена бота @%s.

Это действие:
• выпускает новый токен
• деактивирует все старые токены
• очищает webhook secret

После подтверждения webhook нужно будет перенастроить.`

func msgRevokeDone(username, token string) string {
	return fmt.Sprintf("Готово! Новый токен для @%s:\n`%s`\n\nВсе старые токены деактивированы. Не забудь перенастроить webhook secret.", username, token)
}

const msgRevokeCancelled = "Ротация отменена."

// Generic toggle labels
const lblEnabled = "Включён"
const lblDisabled = "Выключен"
const lblAllowed = "Разрешено"
const lblDisallowed = "Запрещено"

// /setuserpic
const msgSetUserpicSelectBot = "Выбери бота для смены аватара:"
const msgSetUserpicAwait = "Отправь фото — оно станет новым аватаром бота. Принимаются только изображения."
const msgSetUserpicNotPhoto = "Ожидается изображение. Пришли фото или используй /cancel."
const msgSetUserpicNoAttachment = "В сообщении не найдено изображение. Попробуй ещё раз."
const msgSetUserpicUnexpected = "Сейчас не запрошена загрузка аватара. Напиши /setuserpic чтобы начать."

func msgSetUserpicDone(username string) string {
	return fmt.Sprintf("Аватар бота @%s обновлён.", username)
}

