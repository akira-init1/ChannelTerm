package terminal

import (
	"fmt"
	"strings"

	"github.com/akira-init1/ChannelTerm/internal/core/app"
)

// pauseReply keeps protocol fields stable while localizing operator instructions.
type pauseReply struct {
	app.WaitNotice
	Message string `json:"message"`
}
type pausedTaskError struct{ reply pauseReply }

func (e *pausedTaskError) Error() string { return e.reply.Message }
func (e *pausedTaskError) Unwrap() error { return app.ErrAgentPaused }

type waitMessages struct{ pending, allowed, ended string }

var waitTranslations = map[string]waitMessages{
	"en":    {"The user has taken over %s. Wait for the user to continue the AI task, for up to %d seconds. Do not write or retry the previous command. On timeout end this terminal task and await new instructions.", "Read the latest output and obtain current control before continuing. Never replay the interrupted command.", "The wait has ended. Do not write. End this task and await new instructions."},
	"de":    {"Der Benutzer hat %s übernommen. Warten Sie bis zu %d Sekunden auf die Freigabe. Schreiben Sie nicht und wiederholen Sie den Befehl nicht. Beenden Sie die Aufgabe bei Zeitüberschreitung.", "Lesen Sie die aktuelle Ausgabe und übernehmen Sie die aktuelle Steuerung. Wiederholen Sie den alten Befehl nicht.", "Die Wartezeit ist beendet. Beenden Sie die Aufgabe und warten Sie auf neue Anweisungen."},
	"fr":    {"L’utilisateur a repris %s. Attendez son autorisation pendant %d secondes au maximum. N’écrivez rien et ne répétez pas la commande. À l’expiration, terminez cette tâche.", "Lisez la sortie récente et obtenez le contrôle actuel avant de continuer. Ne rejouez pas la commande.", "L’attente est terminée. Terminez la tâche et attendez de nouvelles instructions."},
	"es":    {"El usuario ha tomado el control de %s. Espere su autorización durante un máximo de %d segundos. No escriba ni repita la orden. Si vence el plazo, finalice la tarea.", "Lea la salida reciente y obtenga el control actual antes de continuar. No repita la orden.", "La espera ha terminado. Finalice la tarea y espere nuevas instrucciones."},
	"ja":    {"ユーザーが %s を操作しています。再開の指示を最大 %d 秒待ってください。書き込みや直前のコマンドの再送をせず、時間切れの場合はタスクを終了してください。", "最新の出力と制御状態を確認してから再開してください。以前のコマンドは再送しないでください。", "待機は終了しました。タスクを終了し、新しい指示を待ってください。"},
	"ko":    {"사용자가 %s 제어권을 인수했습니다. 최대 %d초 동안 재개를 기다리세요. 쓰거나 이전 명령을 재시도하지 마세요. 시간이 초과되면 작업을 종료하세요.", "최신 출력과 제어 상태를 확인한 뒤 계속하세요. 이전 명령을 다시 실행하지 마세요.", "대기가 끝났습니다. 작업을 종료하고 새 지시를 기다리세요."},
	"ru":    {"Пользователь управляет %s. Ждите разрешения до %d секунд. Не вводите данные и не повторяйте команду. По истечении срока завершите задачу.", "Прочтите актуальный вывод и получите управление. Не повторяйте прерванную команду.", "Ожидание завершено. Завершите задачу и ждите новых указаний."},
	"zh-CN": {"用户已接管 %s，正在手动操作。请等待用户点击『继续 AI 任务』，最长等待 %d 秒。期间禁止写入，请勿重试上一条命令。若超时，请结束本次终端任务，等待用户发起新指示。", "请先读取最新输出并获取当前控制版本，再继续原任务。不要重放已中断的命令。", "等待已结束。请结束本次终端任务，等待用户发起新指示。"},
	"zh-TW": {"使用者已接管 %s，正在手動操作。請等待使用者點選「繼續 AI 任務」，最多等待 %d 秒。期間禁止寫入，請勿重試上一個命令。若逾時，請結束本次任務，等待新指示。", "請先讀取最新輸出並取得目前的控制權，再繼續任務。請勿重送已中斷的命令。", "等待已結束。請結束本次終端工作，等待使用者發出新指示。"},
}

func localizeWait(n app.WaitNotice) pauseReply {
	lang := n.Locale
	words, ok := waitTranslations[lang]
	if !ok {
		words = waitTranslations["en"]
	}
	seconds := n.Seconds
	format := words.pending
	if (lang == "zh-CN" || lang == "zh-TW") && seconds%60 == 0 {
		unit := "分钟"
		if lang == "zh-TW" {
			unit = "分鐘"
		}
		format = strings.Replace(format, "%d 秒", "%d "+unit, 1)
		seconds /= 60
	}
	message := fmt.Sprintf(format, n.Endpoint, seconds)
	switch n.Status {
	case "allowed":
		message = words.allowed
	case "user_takeover":
	default:
		message = words.ended
	}
	return pauseReply{n, message}
}
