# Sidebox

WindowsまたはmacOSのデスクトップに、デジタル時計と気象庁の天気予報を表示する軽量ウィジェットです。Goを中心に、WindowsではWin32 API、macOSではAppKitを使用します。天気データの取得にAPIキーは不要です。

バージョン: `0.3.0`

リポジトリ: [fukuyori/SideBox](https://github.com/fukuyori/SideBox)

## 表示内容

- 現在時刻（秒単位）、日付、`Sidebox 0.3.0` のバージョン表示
- 今日の天気アイコン、最高気温、最低気温、降水確率、現在の湿度、予報文
- 明日・明後日の天気アイコン、最高気温、最低気温、降水確率、予報文
- 気象庁の予報区域名とデータ提供元

当日の情報を最も大きなカードにまとめ、予報文を気象数値と分離して読みやすくしています。湿度は最新観測値のため、今日のカードだけに表示します。

## 主な機能

- 起動時およびスリープ復帰時の即時更新
- 設定した間隔での定期更新と、通信失敗時の最大12回の再試行
- ドラッグによる移動と表示位置の自動保存・復元
- ウィンドウの辺・四隅を使ったサイズ変更と自動保存・復元
- 常に最前面に表示する設定
- 透明度、地域、更新間隔の設定
- WindowsおよびmacOSのログイン時自動起動設定
- Windowsではタスクバー、macOSではDockに常駐しないウィジェット表示
- Windows版での同一ログインセッション内の二重起動防止

macOS版はAppKit専用のレイアウトです。既存のWindows版は従来のWin32レイアウトで動作します。

## macOS

### 必要な環境

- macOS 11以降（ログイン時自動起動はmacOS 13以降）
- Go 1.24以降
- Xcode Command Line Tools
- 配布用PKGを作成する場合は、Developer ID Application証明書とDeveloper ID Installer証明書
- Apple公証を行う場合は、Keychainに保存した`notarytool`プロファイル

公証プロファイル名は既定で `notarytool` です。別名を使用する場合は `SIDEBOX_NOTARY_PROFILE` で指定します。

### 1. アプリをビルド

```sh
./scripts/build-macos.sh
```

テスト、macOS向けコンパイル、ローカル実行用のアドホック署名を行い、次のアプリを生成します。

```text
dist/Sidebox.app
```

ローカルで確認する場合は、次のように起動できます。

```sh
open dist/Sidebox.app
```

このスクリプトはPKG作成、公証、インストールを行いません。

### 2. 配布用PKGを作成

```sh
./scripts/package-macos.sh
```

既存の `dist/Sidebox.app` を一時領域へ複製し、次の処理を行います。

1. Developer ID Applicationによるアプリ署名
2. Developer ID InstallerによるPKG署名
3. Apple公証サービスへのアップロードと完了待機
4. 公証チケットのステープルとGatekeeper検証

このスクリプトはアプリをビルドせず、インストールも行いません。`VERSION` と入力アプリのバージョンが一致しない場合はエラーで終了します。証明書や公証プロファイルが利用できない場合もPKGは生成されません。

Apple Silicon Macでバージョン0.3.0を作成した場合の出力例です。

```text
dist/Sidebox-0.3.0-macos-arm64.pkg
```

署名IDを明示する場合は、次の環境変数を使用します。

- `SIDEBOX_APP_SIGN_IDENTITY`
- `SIDEBOX_INSTALLER_SIGN_IDENTITY`
- `SIDEBOX_NOTARY_PROFILE`

### 3. インストール

生成されたPKGをFinderから開き、macOS Installerの案内に従ってインストールします。インストール先は次の場所です。

```text
/Applications/Sidebox.app
```

インストールにはmacOSの管理者認証が必要です。Sideboxのスクリプトから管理者権限を要求することはありません。

## Windows

PowerShellでWindows版をビルドして起動します。

```powershell
.\scripts\build.ps1
.\dist\sidebox.exe
```

macOSまたはLinux上でWindows x64版をクロスビルドする場合は、次を実行します。

```sh
./scripts/build.sh
```

Windows Arm64版の場合は、アーキテクチャを指定します。

```sh
GOARCH=arm64 ./scripts/build.sh
```

生成物は `dist/sidebox.exe` です。起動環境にはWindowsが必要です。

## 操作

- ウィジェット上を左ドラッグ: 表示位置を変更
- ウィンドウの辺または四隅をドラッグ: 表示サイズを変更
- 右上の「×」: 終了
- 右クリック: 天気の更新、設定の再読込、設定ファイルを開く、自動起動の切り替え、終了

表示位置とサイズは移動・サイズ変更後に設定ファイルへ保存され、次回起動時に復元されます。

## 設定

設定ファイルは初回起動時に自動作成されます。

- Windows: `%AppData%\sidebox\config.json`
- macOS: `~/Library/Application Support/sidebox/config.json`

共通設定の例です。

```json
{
  "city_code": "140010",
  "refresh_minutes": 15,
  "always_on_top": true,
  "opacity": 0.94,
  "window_x": 32,
  "window_y": 32,
  "window_width": 760,
  "window_height": 425
}
```

`city_code` は気象庁の一次細分区域コードです。`140010` は川崎市を含む神奈川県東部、初期値の `130010` は東京地方です。ほかの地域は[気象庁の地域コード定義](https://www.jma.go.jp/bosai/common/const/area.json)で確認できます。

| 項目 | 内容 |
| --- | --- |
| `refresh_minutes` | 定期更新の間隔。最小5分、初期値15分 |
| `always_on_top` | `true` なら常に最前面、`false` なら通常の重なり順 |
| `opacity` | 透明度。`0.35`から`1.0` |
| `window_x`, `window_y` | 表示位置。移動時に自動保存 |
| `window_width`, `window_height` | 表示サイズ。サイズ変更時に自動保存 |

### OS別の自動起動設定

Windowsでは次の項目を使用します。

```json
"start_with_windows": true
```

macOSでは次の項目を使用します。

```json
"start_at_login": true
```

設定構造はWindowsとmacOSで共用しているため、自動生成された設定ファイルに両方の項目が含まれる場合があります。Windowsは `start_at_login` を無視し、macOSは `start_with_windows` を無視します。

macOSの自動起動はmacOS 13以降で利用でき、Developer ID署名済みアプリを `/Applications` にインストールして使用します。初回は「システム設定」のログイン項目で許可が必要になる場合があります。右クリックメニューの状態は、設定ファイルの値ではなくmacOSへの実際の登録状態を示します。

設定ファイルを編集した後は、ウィジェットを右クリックして「設定を再読込」を選択してください。

## 天気データ

天気予報には[気象庁の府県天気予報データ](https://www.jma.go.jp/bosai/forecast/)を使用します。当日の最高・最低気温が欠ける場合は、同じ予報地点の気象庁アメダス実測値で補完します。湿度も同地点の最新観測値です。

ネットワークに接続できない場合も時計は動作を続け、天気欄にエラーを表示します。
