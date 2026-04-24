with open(r"D:\job\orbit\services\messaging\internal\handler\folder_handler_test.go", "r", encoding="utf-8") as f:
    lines = f.readlines()

out = []
i = 0
while i < len(lines):
    if "fmt.Sprintf" in lines[i] and "U0001F4BC" in lines[i]:
        out.append('\tbodyMap := map[string]interface{}{"title": "Work", "emoticon": "briefcase", "included_chat_ids": []string{chatID}}\n')
        out.append('\tresp := doFolderReq(t, app, http.MethodPost, "/folders", bodyMap, uid.String())\n')
        i += 8
    else:
        out.append(lines[i])
        i += 1

with open(r"D:\job\orbit\services\messaging\internal\handler\folder_handler_test.go", "w", encoding="utf-8") as f:
    f.writelines(out)
print("done lines=" + str(len(out)))
