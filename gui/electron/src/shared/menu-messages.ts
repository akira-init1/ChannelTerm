import type { Messages } from "@lingui/core";
import type { MenuLocale } from "./menus";

// These catalogs contain only literal text. Single-string compiled messages need
// no ICU compiler, extraction plugin, network translation, or dynamic evaluation.
const translations: Record<string, [string, string, string]> = {
  "menu.connection": ["Connection", "连接", "接続"],
  "menu.session": ["Session", "会话", "セッション"],
  "menu.terminal": ["Terminal", "终端", "ターミナル"],
  "menu.view": ["View", "视图", "表示"],
  "menu.settings": ["Settings", "设置", "設定"],
  "menu.help": ["Help", "帮助", "ヘルプ"],
  "menu.language": ["Language", "语言", "言語"],
  "host.connect": ["Connect to Host", "连接到 Host", "Host に接続"],
  "host.disconnect": [
    "Disconnect GUI from Host",
    "断开 GUI 与 Host",
    "GUI を Host から切断",
  ],
  "session.refresh": [
    "Refresh Sessions",
    "刷新会话列表",
    "セッション一覧を更新",
  ],
  "session.attach": ["Attach Session", "附加此会话", "このセッションに接続"],
  "tab.close": [
    "Close tab (detach)",
    "关闭标签（分离）",
    "タブを閉じる（切り離す）",
  ],
  "terminal.clear": [
    "Clear local display",
    "清理本地显示",
    "ローカル表示をクリア",
  ],
  "terminal.focus": ["Focus terminal", "聚焦终端", "ターミナルにフォーカス"],
  "terminal.resume": ["Resume input", "恢复输入", "入力を再開"],
  "view.sidebar": ["Session sidebar", "会话侧边栏", "セッションサイドバー"],
  "language.en": ["English", "English", "English"],
  "language.zh-CN": ["简体中文", "简体中文", "简体中文"],
  "language.ja": ["日本語", "日本語", "日本語"],
  "help.quick": ["Quick help", "使用帮助", "使い方"],
  "app.quit": ["Exit GUI", "退出 GUI", "GUI を終了"],
  "help.title": [
    "Using ChannelTerm",
    "使用 ChannelTerm",
    "ChannelTerm の使い方",
  ],
  "help.connect": [
    "Connect to an existing Host, then select an open Session. Create serial or SSH connections with the CLI.",
    "连接现有 Host，然后选择已打开的会话。串口或 SSH 连接请通过 CLI 创建。",
    "既存の Host に接続して、開いているセッションを選択します。シリアルや SSH 接続は CLI で作成してください。",
  ],
  "help.detach": [
    "Closing a tab detaches only this view. Disconnecting the GUI or exiting it leaves the shared Host and Sessions running.",
    "关闭标签只分离当前视图。断开或退出 GUI 不会关闭共享 Host 和会话。",
    "タブを閉じると、このビューだけが切り離されます。GUI の切断や終了でも共有 Host とセッションは動作を続けます。",
  ],
  "help.clear": [
    "Clear local display removes local scrollback above the current line. It sends no clear/reset command and leaves Host history intact.",
    "清理本地显示会移除当前行上方的本地滚动记录；不会发送 clear/reset 命令，也不会删除 Host 历史。",
    "ローカル表示のクリアは現在行より上のローカル履歴を消去します。clear/reset コマンドは送信せず、Host の履歴も変更しません。",
  ],
  "help.close": ["Close help", "关闭帮助", "ヘルプを閉じる"],
};

export const menuCatalogs: Record<MenuLocale, Messages> = Object.fromEntries(
  (["en", "zh-CN", "ja"] as const).map((locale, index) => [
    locale,
    Object.fromEntries(
      Object.entries(translations).map(([id, values]) => [
        id,
        [values[index]!],
      ]),
    ),
  ]),
) as Record<MenuLocale, Messages>;
