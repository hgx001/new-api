// 一次性修复脚本：把「服务器分配」那批文案从错位状态改回来。
//
// 事故原因见 add-provision-i18n.mjs：ENTRIES 的列是 [key, zh, fr, ja, ru, vi]，
// 但取值用了 entry[langIndex + 1] 而 LANGS 从 en 开始，于是整体右移一位 ——
// en 拿到中文、zh 拿到法文、fr 拿到日文、ja 拿到俄文、ru 拿到越南语、vi 落回中文。
//
// 修复策略：按正确列位算出每个语言的期望值，**只替换确实串了语言的条目**。
// 这样不会覆盖本来就存在的正确翻译（例如 fr 里 "User" 原本就是 Utilisateurs，
// 那次脚本因为键已存在而跳过，没有被污染）。
import fs from 'node:fs'
import path from 'node:path'

const dir = path.join(process.cwd(), 'src/i18n/locales')

/** [key, zh, fr, ja, ru, vi]；key 本身即英文原文。 */
const ENTRIES = [
  ['Server assignment', '服务器分配', 'Attribution de serveur', 'サーバー割り当て', 'Выдача серверов', 'Phân bổ máy chủ'],
  ['Paid orders wait here until you hand over credentials. The rental clock starts at assignment.', '已付款的订单会在这里排队，等你填写登录凭据。租期从分配那一刻才开始算。', 'Les commandes payées attendent ici que vous remettiez les identifiants. Le décompte démarre à l’attribution.', '支払い済みの注文は、認証情報を入力するまでここに並びます。レンタル期間は割り当て時から始まります。', 'Оплаченные заказы ждут здесь, пока вы передадите данные для входа. Аренда начинается с момента выдачи.', 'Đơn đã thanh toán sẽ chờ tại đây cho đến khi bạn bàn giao thông tin đăng nhập. Thời hạn thuê bắt đầu từ lúc phân bổ.'],
  ['Waiting for assignment', '待分配', 'En attente d’attribution', '割り当て待ち', 'Ожидает выдачи', 'Chờ phân bổ'],
  ['Delivered', '已交付', 'Livré', '割り当て済み', 'Выдано', 'Đã bàn giao'],
  ['Longest wait', '最久等待', 'Attente la plus longue', '最長待ち時間', 'Дольше всего ждёт', 'Chờ lâu nhất'],
  ['Just paid', '刚刚支付', 'Vient d’être payé', 'たった今支払い', 'Только что оплачен', 'Vừa thanh toán'],
  ['{{count}} min waiting', '已等待 {{count}} 分钟', 'En attente depuis {{count}} min', '{{count}} 分待ち', 'Ожидает {{count}} мин', 'Đã chờ {{count}} phút'],
  ['{{count}} h waiting', '已等待 {{count}} 小时', 'En attente depuis {{count}} h', '{{count}} 時間待ち', 'Ожидает {{count}} ч', 'Đã chờ {{count}} giờ'],
  ['No orders waiting for assignment', '暂无待分配订单', 'Aucune commande à attribuer', '割り当て待ちの注文はありません', 'Нет заказов на выдачу', 'Không có đơn chờ phân bổ'],
  ['Nothing delivered yet', '还没有交付记录', 'Aucune livraison pour l’instant', 'まだ割り当て実績がありません', 'Пока ничего не выдано', 'Chưa có bàn giao nào'],
  ['New paid orders will show up here automatically.', '新订单支付成功后会出现在这里。', 'Les nouvelles commandes payées apparaîtront ici automatiquement.', '支払い済みの新しい注文はここに自動で表示されます。', 'Новые оплаченные заказы появятся здесь автоматически.', 'Đơn đã thanh toán mới sẽ tự động xuất hiện tại đây.'],
  ['Delivered machines are listed here for your records.', '已交付的机器会留在这里备查。', 'Les machines livrées restent ici pour référence.', '割り当て済みのマシンは記録用にここに残ります。', 'Выданные машины остаются здесь для истории.', 'Máy đã bàn giao được lưu ở đây để tra cứu.'],
  ['Order and user', '订单 / 用户', 'Commande et utilisateur', '注文とユーザー', 'Заказ и пользователь', 'Đơn hàng và người dùng'],
  ['Payment time', '支付时间', 'Heure de paiement', '支払日時', 'Время оплаты', 'Thời gian thanh toán'],
  ['Assign', '分配', 'Attribuer', '割り当て', 'Выдать', 'Phân bổ'],
  ['View instance', '查看实例', 'Voir l’instance', 'インスタンスを見る', 'Открыть инстанс', 'Xem máy chủ'],
  ['Assign this machine', '分配这台服务器', 'Attribuer cette machine', 'このマシンを割り当て', 'Назначить эту машину', 'Phân bổ máy chủ này'],
  ['Machine delivered', '已完成交付', 'Machine livrée', '割り当てが完了しました', 'Машина выдана', 'Đã bàn giao máy'],
  ['The user can see these credentials now. Copy them and send them over if you like.', '用户现在已经能看到这些凭据。可以复制下来发给对方。', 'L’utilisateur voit ces identifiants. Copiez-les et envoyez-les si vous le souhaitez.', 'ユーザーはこの認証情報を確認できるようになりました。必要ならコピーして送信してください。', 'Пользователь уже видит эти данные. Скопируйте и отправьте ему при желании.', 'Người dùng đã có thể xem thông tin này. Hãy sao chép và gửi cho họ nếu cần.'],
  ['Fill in the credentials you prepared. The user sees nothing until you assign.', '填写你准备好的登录信息。在你点「分配」之前，用户界面不会显示任何内容。', 'Renseignez les identifiants préparés. L’utilisateur ne voit rien avant l’attribution.', '用意した認証情報を入力してください。割り当てるまでユーザーには何も表示されません。', 'Введите подготовленные данные для входа. До выдачи пользователь ничего не увидит.', 'Điền thông tin đăng nhập bạn đã chuẩn bị. Người dùng chưa thấy gì cho đến khi bạn phân bổ.'],
  ['User', '用户', 'Utilisateur', 'ユーザー', 'Пользователь', 'Người dùng'],
  ['Order number', '订单号', 'Numéro de commande', '注文番号', 'Номер заказа', 'Mã đơn hàng'],
  ['Configuration', '配置', 'Configuration', '構成', 'Конфигурация', 'Cấu hình'],
  ['Amount paid', '实付金额', 'Montant payé', '支払金額', 'Оплачено', 'Số tiền đã trả'],
  ['Paid at', '支付时间', 'Payé le', '支払日時', 'Время оплаты', 'Thời gian thanh toán'],
  ['Rental clock starts at assignment', '租期从分配那一刻开始计算，排队等待的时间不计费。', 'Le décompte démarre à l’attribution : l’attente n’est pas facturée.', 'レンタル期間は割り当て時から始まります。待ち時間は課金されません。', 'Аренда начинается с момента выдачи: время ожидания не оплачивается.', 'Thời hạn thuê bắt đầu từ lúc phân bổ, thời gian chờ không tính phí.'],
  ['Delivery summary', '交付信息', 'Récapitulatif de livraison', '引き渡し情報', 'Данные выдачи', 'Thông tin bàn giao'],
  ['Copy delivery summary', '复制交付信息', 'Copier le récapitulatif', '引き渡し情報をコピー', 'Скопировать данные выдачи', 'Sao chép thông tin bàn giao'],
  ['Copied', '已复制', 'Copié', 'コピーしました', 'Скопировано', 'Đã sao chép'],
  ['SSH port', 'SSH 端口', 'Port SSH', 'SSH ポート', 'Порт SSH', 'Cổng SSH'],
  ['Generate', '生成', 'Générer', '生成', 'Сгенерировать', 'Tạo'],
  ['Leave empty if you only deliver a key', '只交付密钥时可留空', 'Laisser vide si vous ne livrez qu’une clé', '鍵のみ渡す場合は空欄', 'Оставьте пустым, если выдаёте только ключ', 'Để trống nếu chỉ bàn giao khóa'],
  ['Add an SSH private key (optional)', '添加 SSH 私钥（可选）', 'Ajouter une clé privée SSH (facultatif)', 'SSH 秘密鍵を追加（任意）', 'Добавить приватный ключ SSH (необязательно)', 'Thêm khóa riêng SSH (tùy chọn)'],
  ['Hide private key', '收起私钥', 'Masquer la clé privée', '秘密鍵を隠す', 'Скрыть приватный ключ', 'Ẩn khóa riêng'],
  ['Note for the user (optional)', '给用户的备注（可选）', 'Note pour l’utilisateur (facultatif)', 'ユーザーへのメモ（任意）', 'Заметка для пользователя (необязательно)', 'Ghi chú cho người dùng (tùy chọn)'],
  ['e.g. Ubuntu 22.04, reinstall via ticket', '例如：Ubuntu 22.04，重装请提交工单', 'ex. Ubuntu 22.04, réinstallation via ticket', '例：Ubuntu 22.04、再インストールはチケットで', 'напр. Ubuntu 22.04, переустановка через тикет', 'ví dụ: Ubuntu 22.04, cài lại qua phiếu hỗ trợ'],
  ['e.g. 203.0.113.10', '例如 203.0.113.10', 'ex. 203.0.113.10', '例：203.0.113.10', 'напр. 203.0.113.10', 'ví dụ: 203.0.113.10'],
  ['Please fill in the public IP', '请填写公网 IP', 'Veuillez saisir l’IP publique', 'グローバル IP を入力してください', 'Укажите публичный IP', 'Vui lòng nhập IP công khai'],
  ['Fill in a password or a private key', '密码与私钥至少填写一项', 'Renseignez un mot de passe ou une clé privée', 'パスワードか秘密鍵のいずれかを入力してください', 'Укажите пароль или приватный ключ', 'Nhập mật khẩu hoặc khóa riêng'],
  ['SSH port must be between 1 and 65535', 'SSH 端口需在 1-65535 之间', 'Le port SSH doit être entre 1 et 65535', 'SSH ポートは 1〜65535 の範囲で指定してください', 'Порт SSH должен быть от 1 до 65535', 'Cổng SSH phải trong khoảng 1-65535'],
  ['Assignment failed', '分配失败', 'Échec de l’attribution', '割り当てに失敗しました', 'Не удалось выдать', 'Phân bổ thất bại'],
  ['Done', '完成', 'Terminé', '完了', 'Готово', 'Xong'],
  ['Assign and start billing', '分配并开始计费', 'Attribuer et démarrer la facturation', '割り当てて課金開始', 'Выдать и начать оплату', 'Phân bổ và bắt đầu tính phí'],
  ['Login with the private key you were given', '请使用下发的私钥登录', 'Connectez-vous avec la clé privée fournie', '発行された秘密鍵でログインしてください', 'Войдите с выданным приватным ключом', 'Đăng nhập bằng khóa riêng đã được cấp'],
  ['Not counting yet', '未开始计时', 'Décompte non démarré', 'まだ課金開始前', 'Отсчёт не начат', 'Chưa bắt đầu tính giờ'],
  ['{{count}} instance(s) paid and waiting for assignment. We usually deliver within 30 minutes, and the rental clock only starts once the credentials are handed over.', '{{count}} 台机器已付款，正在等待分配。通常 30 分钟内完成，租期从凭据交付那一刻才开始计算。', '{{count}} instance(s) payée(s) en attente d’attribution. Livraison généralement sous 30 minutes ; le décompte démarre à la remise des identifiants.', '{{count}} 台が支払い済みで割り当て待ちです。通常 30 分以内に完了し、レンタル期間は認証情報の引き渡し時から始まります。', '{{count}} инстанс(ов) оплачено и ожидает выдачи. Обычно это занимает до 30 минут; аренда начинается с передачи данных.', '{{count}} máy đã thanh toán và đang chờ phân bổ. Thường hoàn tất trong 30 phút; thời hạn thuê bắt đầu khi bàn giao thông tin.'],
  ['Payment received. Login credentials appear here as soon as the machine is assigned, and the rental clock starts at that moment.', '已收到付款。管理员分配完成后，登录凭据会显示在这里，租期也从那一刻开始计算。', 'Paiement reçu. Les identifiants apparaîtront dès l’attribution, et le décompte démarrera à ce moment-là.', 'お支払いを受領しました。割り当てが完了すると認証情報がここに表示され、その時点から課金が始まります。', 'Оплата получена. Данные для входа появятся после выдачи, и с этого момента начнётся аренда.', 'Đã nhận thanh toán. Thông tin đăng nhập sẽ hiện ở đây ngay khi phân bổ xong, và thời hạn thuê bắt đầu từ lúc đó.'],
  ['A new server order is waiting for assignment ({{count}} in total)', '有一笔新的服务器订单待分配（共 {{count}} 笔）', 'Nouvelle commande serveur à attribuer ({{count}} au total)', '新しいサーバー注文が割り当て待ちです（合計 {{count}} 件）', 'Новый заказ сервера ждёт выдачи (всего {{count}})', 'Có đơn máy chủ mới chờ phân bổ (tổng {{count}})'],
  ['Refresh', '刷新', 'Rafraîchir', '更新', 'Обновить', 'Làm mới'],
]

/** 列位：0 是英文原文（即 key），1..5 依次对应下面五种语言。 */
const LANGS = ['en', 'zh', 'fr', 'ja', 'ru', 'vi']

const CJK = /[\u4e00-\u9fff]/
const KANA = /[\u3040-\u30ff]/
const CYRILLIC = /[\u0400-\u04ff]/
const VIETNAMESE = /[đĐơƠưƯ\u1ea0-\u1ef9]/

/**
 * 判断某语言下的取值是否「串了别的语言」。
 *
 * 只替换串味的条目：那次脚本会跳过已存在的键，这些键在各语言里本来就是
 * 对的，无条件覆盖会把 Utilisateurs 改成 Utilisateur 之类，制造无谓改动。
 */
function looksForeign(lang, value) {
  switch (lang) {
    case 'en':
      return CJK.test(value) || KANA.test(value) || CYRILLIC.test(value)
    case 'zh':
      // 中文条目一定含汉字；退化成纯拉丁字母说明被写成了法文。
      return !CJK.test(value)
    case 'fr':
      return CJK.test(value) || KANA.test(value) || CYRILLIC.test(value) || VIETNAMESE.test(value)
    case 'ja':
      // 日文本来就用汉字，只把俄文/越南文判为串味。
      return CYRILLIC.test(value) || VIETNAMESE.test(value)
    case 'ru':
      return CJK.test(value) || KANA.test(value) || VIETNAMESE.test(value)
    case 'vi':
      return CJK.test(value) || KANA.test(value) || CYRILLIC.test(value)
    default:
      return false
  }
}

/** 按行替换某个 key 的值，保持文件其余部分（缩进、转义、顺序）原样。 */
function replaceValueLine(lines, key, nextValue) {
  const prefix = `${JSON.stringify(key)}:`
  const hits = []
  for (let i = 0; i < lines.length; i += 1) {
    if (lines[i].trimStart().startsWith(prefix)) hits.push(i)
  }
  if (hits.length !== 1) {
    throw new Error(`key 行匹配数为 ${hits.length}（应为 1）：${key}`)
  }
  const index = hits[0]
  const line = lines[index]
  const indent = line.slice(0, line.length - line.trimStart().length)
  const tail = line.trimEnd().endsWith(',') ? ',' : ''
  lines[index] = `${indent}${prefix} ${JSON.stringify(nextValue)}${tail}`
}

/**
 * translation 对象自身的闭合行。
 *
 * 不能用「文件里最后一个 }」去推：这个文件在 translation 之后还有若干
 * 根级键，闭合行是 `},` 而不是 `}`。按花括号深度扫一遍最稳（跳过字符串
 * 字面量里的括号）。
 */
function findTranslationClose(lines) {
  const startIndex = lines.findIndex((line) =>
    line.trimStart().startsWith('"translation": {')
  )
  if (startIndex === -1) throw new Error('找不到 translation 起始行')
  let depth = 0
  let started = false
  for (let i = startIndex; i < lines.length; i += 1) {
    let inString = false
    let escaped = false
    for (const char of lines[i]) {
      if (escaped) {
        escaped = false
        continue
      }
      if (char === '\\') {
        escaped = true
        continue
      }
      if (char === '"') {
        inString = !inString
        continue
      }
      if (inString) continue
      if (char === '{') {
        depth += 1
        started = true
      } else if (char === '}') {
        depth -= 1
        if (started && depth === 0) return i
      }
    }
  }
  throw new Error('找不到 translation 闭合行')
}

/** 把新键追加为 translation 对象的最后一个属性（它因此不能带尾逗号）。 */
function appendKey(lines, key, value) {
  const close = findTranslationClose(lines)
  let last = close - 1
  while (last >= 0 && lines[last].trim() === '') last -= 1
  if (last < 0) throw new Error('translation 内没有任何属性')
  if (!lines[last].trimEnd().endsWith(',')) lines[last] = `${lines[last].trimEnd()},`

  lines.splice(close, 0, `    ${JSON.stringify(key)}: ${JSON.stringify(value)}`)
}

/**
 * 清掉上一版脚本误插到根层的同名键。
 *
 * 只认「位置在 translation 闭合行之后」的那一条 —— translation 内部如果
 * 已经有同名键，那不是误插，不能删。
 */
function removeStrayRootKey(lines, key) {
  const prefix = `${JSON.stringify(key)}:`
  const close = findTranslationClose(lines)
  const stray = []
  for (let i = close + 1; i < lines.length; i += 1) {
    if (lines[i].trimStart().startsWith(prefix)) stray.push(i)
  }
  if (stray.length === 0) return false
  for (const index of stray.reverse()) {
    lines.splice(index, 1)
    let prev = index - 1
    while (prev >= 0 && lines[prev].trim() === '') prev -= 1
    if (prev >= 0 && lines[prev].trimEnd().endsWith(',')) {
      lines[prev] = lines[prev].trimEnd().replace(/,$/, '')
    }
  }
  return true
}

let totalFixed = 0
for (let langIndex = 0; langIndex < LANGS.length; langIndex += 1) {
  const lang = LANGS[langIndex]
  const file = path.join(dir, `${lang}.json`)
  const raw = fs.readFileSync(file, 'utf8')
  const parsed = JSON.parse(raw)
  const existing = parsed.translation ?? {}
  const lines = raw.split('\n')

  let added = 0
  let fixed = 0
  let cleaned = 0
  for (const entry of ENTRIES) {
    const key = entry[0]
    const expected = langIndex === 0 ? key : entry[langIndex]
    if (removeStrayRootKey(lines, key)) cleaned += 1
    const current = existing[key]

    if (current === undefined) {
      // 补上缺的键（含这次新加的 IP 示例文案）
      appendKey(lines, key, expected)
      added += 1
      continue
    }
    if (current === expected) continue
    if (!looksForeign(lang, current)) continue
    replaceValueLine(lines, key, expected)
    fixed += 1
  }

  if (added === 0 && fixed === 0 && cleaned === 0) {
    console.log(`[${lang}] 无需修改`)
    continue
  }
  const output = lines.join('\n')
  JSON.parse(output)
  fs.writeFileSync(file, output, 'utf8')
  totalFixed += fixed
  console.log(`[${lang}] 修复 ${fixed} 条，补录 ${added} 条，清理误插 ${cleaned} 条`)
}

console.log(`\n合计修复 ${totalFixed} 条错位文案`)
