// 一次性脚本：替换到期提示文案 + 新增「转为包月」条目。
// 逐行改写，不做 JSON 全量重排 —— 全量 stringify 会把 \uXXXX 转义打散、
// 并改写重复 key 的顺序。
import fs from 'node:fs'
import path from 'node:path'

const dir = path.join(process.cwd(), 'src/i18n/locales')

const OLD_PREFIX =
  '    "Instances are released automatically once the rental period ends.'

const NEW_KEY =
  'Instances stop as soon as the rental period ends and stay recoverable for 7 days — renew within that window and they come back. After 7 days they are released and all data is deleted.'

const NEW_VALUE = {
  en: NEW_KEY,
  zh: '实例到期后会立即停机，机器保留 7 天：期间续费即可恢复；7 天后自动释放，数据全部清空且无法恢复。',
  fr: "L'instance s'arrête dès la fin de la période de location et reste récupérable pendant 7 jours — renouvelez dans ce délai pour la réactiver. Passé 7 jours, elle est libérée et toutes les données sont supprimées.",
  ja: 'インスタンスはレンタル期間の終了と同時に停止し、7 日間は復旧できます。その期間内に更新すれば元に戻ります。7 日を過ぎると解放され、データはすべて削除されます。',
  ru: 'По окончании аренды инстанс сразу останавливается и остаётся восстанавливаемым в течение 7 дней — продлите в этот срок, и он вернётся. Через 7 дней он освобождается, все данные удаляются.',
  vi: 'Instance sẽ dừng ngay khi hết hạn thuê và vẫn có thể khôi phục trong 7 ngày — gia hạn trong khoảng thời gian này để dùng lại. Sau 7 ngày, instance được giải phóng và toàn bộ dữ liệu bị xóa.',
}

const MONTHLY_KEY = 'Switches to monthly billing'
const MONTHLY_VALUE = {
  en: 'Switches to monthly billing',
  zh: '转为包月计费',
  fr: 'Passe en facturation mensuelle',
  ja: '月額課金に切り替え',
  ru: 'Переход на месячную оплату',
  vi: 'Chuyển sang tính phí theo tháng',
}

for (const [lang, value] of Object.entries(NEW_VALUE)) {
  const file = path.join(dir, `${lang}.json`)
  const raw = fs.readFileSync(file, 'utf8')
  const lines = raw.split('\n')
  const idx = lines.findIndex((line) => line.startsWith(OLD_PREFIX))
  if (idx === -1) {
    console.log(`[${lang}] SKIP：没找到到期提示行`)
    continue
  }
  const comma = lines[idx].trimEnd().endsWith(',') ? ',' : ''
  // JSON 字符串里不能直接出现未转义的换行/引号；这批文案没有双引号
  lines[idx] =
    `    ${JSON.stringify(NEW_KEY)}: ${JSON.stringify(value)}${comma}`

  const monthly = `    ${JSON.stringify(MONTHLY_KEY)}: ${JSON.stringify(MONTHLY_VALUE[lang])},`
  const already = lines.some((line) => line.startsWith(`    "${MONTHLY_KEY}"`))
  if (!already) lines.splice(idx + 1, 0, monthly)

  fs.writeFileSync(file, lines.join('\n'), 'utf8')
  console.log(`[${lang}] OK：替换到期文案${already ? '' : ' + 新增包月提示'}`)
}
