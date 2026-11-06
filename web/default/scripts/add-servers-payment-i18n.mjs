import fs from 'fs'

/**
 * 一次性脚本：把 servers 功能新增的 11 条文案补进 6 个语言包。
 *
 * 注意：这里用**行级文本插入**而不是 JSON.parse/stringify 全量重写 ——
 * 语言包里有 \uXXXX 转义写法的键，也有重复键（Previous page / Next page），
 * 全量重写会把它们改写/去重，产生上千行无意义 diff。
 * 行级插入只动新增的那几行，原文件其余部分一字不改。
 */

const additions = {
  en: {
    'Review the configuration before paying.':
      'Review the configuration before paying.',
    'Pay directly with {{method}}': 'Pay directly with {{method}}',
    'You will be redirected to the payment page. The server is provisioned once the payment is confirmed.':
      'You will be redirected to the payment page. The server is provisioned once the payment is confirmed.',
    'Payment is confirmed by the payment gateway':
      'Payment is confirmed by the payment gateway',
    'Payment page opened. Your server will be ready once the payment is confirmed.':
      'Payment page opened. Your server will be ready once the payment is confirmed.',
    'Failed to open the payment page': 'Failed to open the payment page',
    'Insufficient balance. Top up {{amount}} more to place this order.':
      'Insufficient balance. Top up {{amount}} more to place this order.',
    'The price has been updated by the server; the amount below is the final one.':
      'The price has been updated by the server; the amount below is the final one.',
    'Estimated with the price you paid; the server confirms the final amount.':
      'Estimated with the price you paid; the server confirms the final amount.',
    'The new period starts from the later of now and the current expiry time.':
      'The new period starts from the later of now and the current expiry time.',
    'The unused part of what you paid is refunded to your balance, calculated by the server from the actual amount paid and the remaining time.':
      'The unused part of what you paid is refunded to your balance, calculated by the server from the actual amount paid and the remaining time.',
  },
  zh: {
    'Review the configuration before paying.': '请在支付前确认配置信息。',
    'Pay directly with {{method}}': '使用{{method}}直接支付',
    'You will be redirected to the payment page. The server is provisioned once the payment is confirmed.':
      '你将被跳转到支付页面，支付确认后自动开通服务器。',
    'Payment is confirmed by the payment gateway': '支付结果由支付网关确认',
    'Payment page opened. Your server will be ready once the payment is confirmed.':
      '已打开支付页面，支付确认后服务器即可使用。',
    'Failed to open the payment page': '无法打开支付页面',
    'Insufficient balance. Top up {{amount}} more to place this order.':
      '余额不足，还需充值 {{amount}} 才能下单。',
    'The price has been updated by the server; the amount below is the final one.':
      '服务器已更新价格，下方金额为最终价格。',
    'Estimated with the price you paid; the server confirms the final amount.':
      '按你支付时的价格估算，最终金额以服务器为准。',
    'The new period starts from the later of now and the current expiry time.':
      '新的租期从「当前时间」与「原到期时间」中较晚者开始计算。',
    'The unused part of what you paid is refunded to your balance, calculated by the server from the actual amount paid and the remaining time.':
      '未使用的部分将退回余额，由服务器按实际支付金额与剩余时长计算。',
  },
  ja: {
    'Review the configuration before paying.':
      '支払い前に構成を確認してください。',
    'Pay directly with {{method}}': '{{method}}で直接支払う',
    'You will be redirected to the payment page. The server is provisioned once the payment is confirmed.':
      '支払いページへ移動します。支払いが確認された時点でサーバーが開通します。',
    'Payment is confirmed by the payment gateway':
      '支払いの確認は決済ゲートウェイが行います',
    'Payment page opened. Your server will be ready once the payment is confirmed.':
      '支払いページを開きました。支払いが確認され次第、サーバーが利用可能になります。',
    'Failed to open the payment page': '支払いページを開けませんでした',
    'Insufficient balance. Top up {{amount}} more to place this order.':
      '残高が不足しています。{{amount}}をチャージしてから注文してください。',
    'The price has been updated by the server; the amount below is the final one.':
      'サーバーが価格を更新しました。以下の金額が最終金額です。',
    'Estimated with the price you paid; the server confirms the final amount.':
      '支払った時点の価格で試算しています。最終金額はサーバーが確定します。',
    'The new period starts from the later of now and the current expiry time.':
      '新しい期間は「現在時刻」と「現在の有効期限」のうち遅い方から開始します。',
    'The unused part of what you paid is refunded to your balance, calculated by the server from the actual amount paid and the remaining time.':
      '未使用分は残高に返金されます。実際の支払金額と残り時間からサーバーが計算します。',
  },
  fr: {
    'Review the configuration before paying.':
      'Vérifiez la configuration avant de payer.',
    'Pay directly with {{method}}': 'Payer directement avec {{method}}',
    'You will be redirected to the payment page. The server is provisioned once the payment is confirmed.':
      'Vous serez redirigé vers la page de paiement. Le serveur est provisionné une fois le paiement confirmé.',
    'Payment is confirmed by the payment gateway':
      'Le paiement est confirmé par la passerelle de paiement',
    'Payment page opened. Your server will be ready once the payment is confirmed.':
      'Page de paiement ouverte. Votre serveur sera prêt dès que le paiement sera confirmé.',
    'Failed to open the payment page':
      "Impossible d'ouvrir la page de paiement",
    'Insufficient balance. Top up {{amount}} more to place this order.':
      'Solde insuffisant. Rechargez {{amount}} pour passer cette commande.',
    'The price has been updated by the server; the amount below is the final one.':
      'Le serveur a mis à jour le prix ; le montant ci-dessous est le montant final.',
    'Estimated with the price you paid; the server confirms the final amount.':
      'Estimé au prix payé ; le serveur confirme le montant final.',
    'The new period starts from the later of now and the current expiry time.':
      "La nouvelle période commence à la plus tardive entre maintenant et l'expiration actuelle.",
    'The unused part of what you paid is refunded to your balance, calculated by the server from the actual amount paid and the remaining time.':
      'La part inutilisée est remboursée sur votre solde, calculée par le serveur à partir du montant réellement payé et du temps restant.',
  },
  ru: {
    'Review the configuration before paying.':
      'Проверьте конфигурацию перед оплатой.',
    'Pay directly with {{method}}': 'Оплатить напрямую через {{method}}',
    'You will be redirected to the payment page. The server is provisioned once the payment is confirmed.':
      'Вы будете перенаправлены на страницу оплаты. Сервер создаётся после подтверждения платежа.',
    'Payment is confirmed by the payment gateway':
      'Платёж подтверждается платёжным шлюзом',
    'Payment page opened. Your server will be ready once the payment is confirmed.':
      'Страница оплаты открыта. Сервер будет готов после подтверждения платежа.',
    'Failed to open the payment page': 'Не удалось открыть страницу оплаты',
    'Insufficient balance. Top up {{amount}} more to place this order.':
      'Недостаточно баланса. Пополните ещё {{amount}} для оформления заказа.',
    'The price has been updated by the server; the amount below is the final one.':
      'Сервер обновил цену; итоговая сумма указана ниже.',
    'Estimated with the price you paid; the server confirms the final amount.':
      'Рассчитано по оплаченной цене; итоговую сумму подтверждает сервер.',
    'The new period starts from the later of now and the current expiry time.':
      'Новый период начинается с более позднего из «сейчас» и текущего срока окончания.',
    'The unused part of what you paid is refunded to your balance, calculated by the server from the actual amount paid and the remaining time.':
      'Неиспользованная часть возвращается на баланс; сервер рассчитывает её по фактически оплаченной сумме и оставшемуся времени.',
  },
  vi: {
    'Review the configuration before paying.':
      'Vui lòng kiểm tra cấu hình trước khi thanh toán.',
    'Pay directly with {{method}}': 'Thanh toán trực tiếp bằng {{method}}',
    'You will be redirected to the payment page. The server is provisioned once the payment is confirmed.':
      'Bạn sẽ được chuyển đến trang thanh toán. Máy chủ được cấp phát sau khi thanh toán được xác nhận.',
    'Payment is confirmed by the payment gateway':
      'Thanh toán được xác nhận bởi cổng thanh toán',
    'Payment page opened. Your server will be ready once the payment is confirmed.':
      'Đã mở trang thanh toán. Máy chủ sẽ sẵn sàng khi thanh toán được xác nhận.',
    'Failed to open the payment page': 'Không thể mở trang thanh toán',
    'Insufficient balance. Top up {{amount}} more to place this order.':
      'Số dư không đủ. Hãy nạp thêm {{amount}} để đặt đơn hàng này.',
    'The price has been updated by the server; the amount below is the final one.':
      'Máy chủ đã cập nhật giá; số tiền bên dưới là số tiền cuối cùng.',
    'Estimated with the price you paid; the server confirms the final amount.':
      'Ước tính theo giá bạn đã thanh toán; máy chủ xác nhận số tiền cuối cùng.',
    'The new period starts from the later of now and the current expiry time.':
      'Kỳ hạn mới bắt đầu từ thời điểm muộn hơn trong hai mốc: hiện tại và thời điểm hết hạn hiện tại.',
    'The unused part of what you paid is refunded to your balance, calculated by the server from the actual amount paid and the remaining time.':
      'Phần chưa sử dụng được hoàn lại số dư, do máy chủ tính từ số tiền thực trả và thời gian còn lại.',
  },
}

for (const [locale, entries] of Object.entries(additions)) {
  const file = `src/i18n/locales/${locale}.json`
  const raw = fs.readFileSync(file, 'utf8')
  const parsed = JSON.parse(raw)
  const root = parsed.translation

  // 只插入还不存在的键
  const pending = Object.entries(entries).filter(([k]) => !(k in root))
  if (pending.length === 0) {
    console.log(`[${locale}] 全部已存在，跳过`)
    continue
  }

  const lines = raw.split('\n')
  // 找到 `"translation": {` 所在行，在它后面插入
  const anchor = lines.findIndex((l) => /^\s*"translation"\s*:\s*\{/.test(l))
  if (anchor < 0) throw new Error(`${locale}: 找不到 translation 锚点`)

  const indent = (lines[anchor].match(/^\s*/) ?? [''])[0] + '  '
  const inserted = pending.map(
    ([k, v]) => `${indent}${JSON.stringify(k)}: ${JSON.stringify(v)},`
  )
  lines.splice(anchor + 1, 0, ...inserted)

  const out = lines.join('\n')
  // 校验：插完之后仍然是合法 JSON，且键值都在 translation 下
  const check = JSON.parse(out)
  for (const [k, v] of pending) {
    if (check.translation[k] !== v)
      throw new Error(`${locale}: 键写入失败 ${k}`)
  }

  fs.writeFileSync(file, out, 'utf8')
  console.log(`[${locale}] inserted ${pending.length} keys`)
}
