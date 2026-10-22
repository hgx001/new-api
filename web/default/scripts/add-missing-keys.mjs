import fs from 'node:fs/promises'
import path from 'node:path'

const LOCALES_DIR = path.resolve('src/i18n/locales')

// Keep the same obfuscated-key escaping that scripts/sync-i18n.mjs applies,
// otherwise this script would silently de-obfuscate the key inside the file.
const OBFUSCATED_KEYS = [
  {
    runtime: ['footer', 'new' + 'api', 'projectAttributionSuffix'].join('.'),
    serialized: 'footer.new\\u0061pi.projectAttributionSuffix',
  },
]

function stableStringify(obj) {
  let text = JSON.stringify(obj, null, 2)
  for (const key of OBFUSCATED_KEYS) {
    text = text.replaceAll(`"${key.runtime}":`, `"${key.serialized}":`)
  }
  return text + '\n'
}

const newKeys = {
  en: {
    '+{{price}} / month': '+{{price}} / month',
    '≈ {{amount}} / month': '≈ {{amount}} / month',
    '5 Mbps dedicated': '5 Mbps dedicated',
    '10 Mbps dedicated': '10 Mbps dedicated',
    '20 Mbps dedicated': '20 Mbps dedicated',
    '50 Mbps dedicated': '50 Mbps dedicated',
    '100 Mbps dedicated': '100 Mbps dedicated',
    '6 months': '6 months',
    '1 year': '1 year',
    '3 years': '3 years',
    '99.9%': '99.9%',
    '100 Mbps': '100 Mbps',
    'After the payment succeeds the server is provisioned automatically, and the IP address, username and password appear under My servers.':
      'After the payment succeeds the server is provisioned automatically, and the IP address, username and password appear under My servers.',
    'At least 8 characters': 'At least 8 characters',
    Bandwidth: 'Bandwidth',
    'Balanced compute and memory for most workloads':
      'Balanced compute and memory for most workloads',
    BGP: 'BGP',
    'Buy now · {{amount}}': 'Buy now · {{amount}}',
    'Can I change the password after the server is created?':
      'Can I change the password after the server is created?',
    'Can I switch between Guangzhou and Shanghai?':
      'Can I switch between Guangzhou and Shanghai?',
    'Change password': 'Change password',
    'Change root password': 'Change root password',
    'Choose a family that matches your workload':
      'Choose a family that matches your workload',
    'Cloud Servers': 'Cloud Servers',
    'Compute Optimized': 'Compute Optimized',
    'Configure a server': 'Configure a server',
    'Configure a server and complete the payment. The instance details will show up here.':
      'Configure a server and complete the payment. The instance details will show up here.',
    'Configure server': 'Configure server',
    'Configure your server': 'Configure your server',
    'Confirm your order': 'Confirm your order',
    'Copy failed, please select and copy manually':
      'Copy failed, please select and copy manually',
    'Copy SSH command': 'Copy SSH command',
    'Dedicated GPU for inference and rendering':
      'Dedicated GPU for inference and rendering',
    'Dedicated inbound and outbound bandwidth':
      'Dedicated inbound and outbound bandwidth',
    'DDoS protection included': 'DDoS protection included',
    'East China hub with the strongest international routing':
      'East China hub with the strongest international routing',
    'Encrypted checkout': 'Encrypted checkout',
    'Engineer support': 'Engineer support',
    'Entry-level instances for websites, blogs and small services. Instant provisioning with a public IPv4 address.':
      'Entry-level instances for websites, blogs and small services. Instant provisioning with a public IPv4 address.',
    'Everything you need to run production workloads, without the setup overhead.':
      'Everything you need to run production workloads, without the setup overhead.',
    'Every instance comes with network-level traffic cleaning and a free firewall rule set.':
      'Every instance comes with network-level traffic cleaning and a free firewall rule set.',
    'Expires on': 'Expires on',
    'Failed to update the password': 'Failed to update the password',
    'From {{price}} / month': 'From {{price}} / month',
    'Frequently asked questions': 'Frequently asked questions',
    'GPU Accelerated': 'GPU Accelerated',
    'GPU accelerated instances with dedicated graphics cards for model inference, rendering and video processing.':
      'GPU accelerated instances with dedicated graphics cards for model inference, rendering and video processing.',
    'General Purpose': 'General Purpose',
    'General purpose instances for APIs, CI runners and container hosts. SSD storage and private networking included.':
      'General purpose instances for APIs, CI runners and container hosts. SSD storage and private networking included.',
    Guangzhou: 'Guangzhou',
    'High frequency cores for heavy computation':
      'High frequency cores for heavy computation',
    'How long does it take to get my server?':
      'How long does it take to get my server?',
    Included: 'Included',
    'Instant delivery': 'Instant delivery',
    'Instances are created automatically after the payment succeeds. Most orders are ready within five minutes, and the IP address, username and password appear under My servers as soon as provisioning finishes.':
      'Instances are created automatically after the payment succeeds. Most orders are ready within five minutes, and the IP address, username and password appear under My servers as soon as provisioning finishes.',
    'Instances are provisioned automatically right after the payment is confirmed, usually within five minutes.':
      'Instances are provisioned automatically right after the payment is confirmed, usually within five minutes.',
    'Large memory pools for databases and caching':
      'Large memory pools for databases and caching',
    Lite: 'Lite',
    'Memory Optimized': 'Memory Optimized',
    'Memory optimized instances for MySQL, PostgreSQL, Elasticsearch and in-memory caches. Large NVMe volumes.':
      'Memory optimized instances for MySQL, PostgreSQL, Elasticsearch and in-memory caches. Large NVMe volumes.',
    'Most popular': 'Most popular',
    'Multi-line network': 'Multi-line network',
    'My servers ({{count}})': 'My servers ({{count}})',
    'Network error, please try again later':
      'Network error, please try again later',
    New: 'New',
    'New: cloud servers are now available':
      'New: cloud servers are now available',
    'No servers yet': 'No servers yet',
    'Peak bandwidth': 'Peak bandwidth',
    'Password must be at least 8 characters':
      'Password must be at least 8 characters',
    'Password updated': 'Password updated',
    'Pay {{amount}}': 'Pay {{amount}}',
    'Payment failed, please try again': 'Payment failed, please try again',
    'Payment successful': 'Payment successful',
    'Payment successful, your server is being provisioned':
      'Payment successful, your server is being provisioned',
    'Pick a region, a plan, the specs and the bandwidth you need. Pay for 6 months, 1 year or 3 years, and get your server credentials right after the payment succeeds.':
      'Pick a region, a plan, the specs and the bandwidth you need. Pay for 6 months, 1 year or 3 years, and get your server credentials right after the payment succeeds.',
    'Please sign in before ordering': 'Please sign in before ordering',
    'Prices shown include taxes and are settled in CNY.':
      'Prices shown include taxes and are settled in CNY.',
    'Processing payment...': 'Processing payment...',
    Provisioning: 'Provisioning',
    'Public IP': 'Public IP',
    Region: 'Region',
    'Reliable cloud servers for your next project':
      'Reliable cloud servers for your next project',
    'Repeat the new password': 'Repeat the new password',
    'Review the configuration and choose a payment method.':
      'Review the configuration and choose a payment method.',
    'SSH command copied': 'SSH command copied',
    'Save 15%': 'Save 15%',
    'Save 28%': 'Save 28%',
    'Save {{amount}}': 'Save {{amount}}',
    'Server: {{server}}': 'Server: {{server}}',
    'Sign in to order': 'Sign in to order',
    'South China hub with direct access to mainland networks':
      'South China hub with direct access to mainland networks',
    Specifications: 'Specifications',
    Term: 'Term',
    'The two passwords do not match': 'The two passwords do not match',
    'The instance is kept for seven days after the expiry date so you can renew it. After that the data is released and the instance is reclaimed.':
      'The instance is kept for seven days after the expiry date so you can renew it. After that the data is released and the instance is reclaimed.',
    'Toggle password visibility': 'Toggle password visibility',
    'Total price': 'Total price',
    'Uptime SLA': 'Uptime SLA',
    'Update password': 'Update password',
    'Upgrade any time': 'Upgrade any time',
    'vCPU, memory and system disk': 'vCPU, memory and system disk',
    'View my servers': 'View my servers',
    'What happens when the subscription expires?':
      'What happens when the subscription expires?',
    'Where the server is located': 'Where the server is located',
    'Why choose our cloud servers': 'Why choose our cloud servers',
    'Yes. Both regions run the same images and the same plan catalogue, so you can move an instance by rebuilding it in the other region.':
      'Yes. Both regions run the same images and the same plan catalogue, so you can move an instance by rebuilding it in the other region.',
    'Yes. Open My servers, find the instance and use the Change password action. The new password takes effect on the next login.':
      'Yes. Open My servers, find the instance and use the Change password action. The new password takes effect on the next login.',
    'You pay': 'You pay',
    'Your server is being provisioned. The IP address, username and password are available under My servers.':
      'Your server is being provisioned. The IP address, username and password are available under My servers.',
    'Best for personal projects and blogs':
      'Best for personal projects and blogs',
    'Change plan, specs or bandwidth and the remaining period is charged at a prorated price.':
      'Change plan, specs or bandwidth and the remaining period is charged at a prorated price.',
    'Compute optimized instances for batch jobs, model inference and data processing. High clock speed vCPUs.':
      'Compute optimized instances for batch jobs, model inference and data processing. High clock speed vCPUs.',
    Shanghai: 'Shanghai',
    'Talk to the engineers who run the platform instead of a generic ticket queue.':
      'Talk to the engineers who run the platform instead of a generic ticket queue.',
    '{{days}} days left': '{{days}} days left',
    'Click to enlarge': 'Click to enlarge',
    'Enlarge QR code': 'Enlarge QR code',
    'Save QR code': 'Save QR code',
    'Save it to your album and scan later':
      'Save it to your album and scan later',
    'WeChat ID': 'WeChat ID',
    'Demo payment (no real charge)': 'Demo payment (no real charge)',
    'Demo mode: the payment is simulated and no real money is charged.':
      'Demo mode: the payment is simulated and no real money is charged.',
    'Online payment is not available yet, please contact the administrator':
      'Online payment is not available yet, please contact the administrator',
  },
  zh: {
    '+{{price}} / month': '+{{price}} / 月',
    '≈ {{amount}} / month': '约 {{amount}} / 月',
    '5 Mbps dedicated': '独享 5 Mbps',
    '10 Mbps dedicated': '独享 10 Mbps',
    '20 Mbps dedicated': '独享 20 Mbps',
    '50 Mbps dedicated': '独享 50 Mbps',
    '100 Mbps dedicated': '独享 100 Mbps',
    '6 months': '半年',
    '1 year': '一年',
    '3 years': '三年',
    '99.9%': '99.9%',
    '100 Mbps': '100 Mbps',
    'After the payment succeeds the server is provisioned automatically, and the IP address, username and password appear under My servers.':
      '支付成功后系统自动开通服务器，IP 地址、用户名与密码可在「我的服务器」中查看。',
    'At least 8 characters': '至少 8 个字符',
    Bandwidth: '带宽',
    'Balanced compute and memory for most workloads':
      '算力与内存均衡，适合绝大多数业务场景',
    BGP: 'BGP',
    'Buy now · {{amount}}': '立即购买 · {{amount}}',
    'Can I change the password after the server is created?':
      '服务器创建后可以修改密码吗？',
    'Can I switch between Guangzhou and Shanghai?':
      '广州和上海之间可以互相切换吗？',
    'Change password': '修改密码',
    'Change root password': '修改 root 密码',
    'Choose a family that matches your workload':
      '选择与你的业务负载匹配的型号',
    'Cloud Servers': '服务器租赁',
    'Compute Optimized': '计算型',
    'Configure a server': '去配置服务器',
    'Configure a server and complete the payment. The instance details will show up here.':
      '配置服务器并完成支付后，实例信息会显示在这里。',
    'Configure server': '配置服务器',
    'Configure your server': '开始配置服务器',
    'Confirm your order': '确认订单',
    'Copy failed, please select and copy manually': '复制失败，请手动选中复制',
    'Copy SSH command': '复制 SSH 命令',
    'Dedicated GPU for inference and rendering': '独立显卡，适合推理与渲染',
    'Dedicated inbound and outbound bandwidth': '独享进出带宽',
    'DDoS protection included': '内置 DDoS 防护',
    'East China hub with the strongest international routing':
      '华东枢纽，国际线路质量最好',
    'Encrypted checkout': '加密收银台',
    'Engineer support': '工程师支持',
    'Entry-level instances for websites, blogs and small services. Instant provisioning with a public IPv4 address.':
      '入门型实例，适合网站、博客与小型服务。开通即带公网 IPv4 地址。',
    'Everything you need to run production workloads, without the setup overhead.':
      '承载生产业务所需的一切，免去繁琐的部署准备。',
    'Every instance comes with network-level traffic cleaning and a free firewall rule set.':
      '每台实例自带网络层流量清洗与免费防火墙规则集。',
    'Expires on': '到期时间',
    'Failed to update the password': '修改密码失败',
    'From {{price}} / month': '{{price}} / 月起',
    'Frequently asked questions': '常见问题',
    'GPU Accelerated': 'GPU 型',
    'GPU accelerated instances with dedicated graphics cards for model inference, rendering and video processing.':
      'GPU 加速实例，配备独立显卡，适合模型推理、渲染与视频处理。',
    'General Purpose': '通用型',
    'General purpose instances for APIs, CI runners and container hosts. SSD storage and private networking included.':
      '通用型实例，适合 API、CI 构建机与容器宿主机。自带 SSD 与内网互通。',
    Guangzhou: '广州',
    'High frequency cores for heavy computation': '高主频核心，适合重度计算',
    'How long does it take to get my server?': '多久能拿到服务器？',
    Included: '已包含',
    'Instant delivery': '极速交付',
    'Instances are created automatically after the payment succeeds. Most orders are ready within five minutes, and the IP address, username and password appear under My servers as soon as provisioning finishes.':
      '支付成功后实例自动创建，绝大多数订单 5 分钟内就绪，IP 地址、用户名与密码会在开通完成后出现在「我的服务器」里。',
    'Instances are provisioned automatically right after the payment is confirmed, usually within five minutes.':
      '支付确认后自动开通，通常 5 分钟内交付。',
    'Large memory pools for databases and caching':
      '大内存池，适合数据库与缓存',
    Lite: '轻量型',
    'Memory Optimized': '内存型',
    'Memory optimized instances for MySQL, PostgreSQL, Elasticsearch and in-memory caches. Large NVMe volumes.':
      '内存优化实例，适合 MySQL、PostgreSQL、Elasticsearch 与内存缓存，配备大容量 NVMe。',
    'Most popular': '最受欢迎',
    'Multi-line network': '多线 BGP',
    'My servers ({{count}})': '我的服务器（{{count}}）',
    'Network error, please try again later': '网络错误，请稍后重试',
    New: '新品',
    'New: cloud servers are now available': '全新上线：云服务器已开放购买',
    'No servers yet': '还没有服务器',
    'Peak bandwidth': '峰值带宽',
    'Password must be at least 8 characters': '密码必须至少 8 个字符',
    'Password updated': '密码已更新',
    'Pay {{amount}}': '支付 {{amount}}',
    'Payment failed, please try again': '支付失败，请重试',
    'Payment successful': '支付成功',
    'Payment successful, your server is being provisioned':
      '支付成功，服务器正在开通中',
    'Pick a region, a plan, the specs and the bandwidth you need. Pay for 6 months, 1 year or 3 years, and get your server credentials right after the payment succeeds.':
      '选择地区、型号、配置与带宽，半年 / 一年 / 三年任选，支付成功后立即拿到服务器账号信息。',
    'Please sign in before ordering': '请先登录后再下单',
    'Prices shown include taxes and are settled in CNY.':
      '价格含税，以人民币结算。',
    'Processing payment...': '正在处理支付…',
    Provisioning: '开通时效',
    'Public IP': '公网 IP',
    Region: '地区',
    'Reliable cloud servers for your next project':
      '为你的下一个项目准备一台可靠的云服务器',
    'Repeat the new password': '再次输入新密码',
    'Review the configuration and choose a payment method.':
      '核对配置信息并选择支付方式。',
    'SSH command copied': 'SSH 命令已复制',
    'Save 15%': '省 15%',
    'Save 28%': '省 28%',
    'Save {{amount}}': '省 {{amount}}',
    'Server: {{server}}': '服务器：{{server}}',
    'Sign in to order': '登录后下单',
    'South China hub with direct access to mainland networks':
      '华南枢纽，直连大陆网络',
    Specifications: '配置',
    Term: '购买周期',
    'The two passwords do not match': '两次输入的密码不一致',
    'The instance is kept for seven days after the expiry date so you can renew it. After that the data is released and the instance is reclaimed.':
      '到期后实例保留 7 天供你续费，逾期数据将被释放、实例回收。',
    'Toggle password visibility': '显示 / 隐藏密码',
    'Total price': '总价',
    'Uptime SLA': 'SLA 可用性',
    'Update password': '更新密码',
    'Upgrade any time': '随时升配',
    'vCPU, memory and system disk': 'vCPU、内存与系统盘',
    'View my servers': '查看我的服务器',
    'What happens when the subscription expires?': '订阅到期后会怎样？',
    'Where the server is located': '服务器所在机房',
    'Why choose our cloud servers': '为什么选择我们的云服务器',
    'Yes. Both regions run the same images and the same plan catalogue, so you can move an instance by rebuilding it in the other region.':
      '可以。两个地区使用相同的镜像与相同的型号目录，在另一地区重建即可完成迁移。',
    'Yes. Open My servers, find the instance and use the Change password action. The new password takes effect on the next login.':
      '可以。进入「我的服务器」找到实例，点击「修改密码」，新密码在下次登录时生效。',
    'You pay': '应付金额',
    'Your server is being provisioned. The IP address, username and password are available under My servers.':
      '服务器正在开通中，IP 地址、用户名与密码可在「我的服务器」中查看。',
    'Best for personal projects and blogs': '适合个人项目与博客',
    'Change plan, specs or bandwidth and the remaining period is charged at a prorated price.':
      '变更型号、配置或带宽后，剩余周期按比例折算补差价。',
    'Compute optimized instances for batch jobs, model inference and data processing. High clock speed vCPUs.':
      '计算优化实例，适合批处理任务、模型推理与数据处理，配备高主频 vCPU。',
    Shanghai: '上海',
    'Talk to the engineers who run the platform instead of a generic ticket queue.':
      '直接和运营平台的工程师沟通，而不是在通用工单队列里排队。',
    '{{days}} days left': '剩余 {{days}} 天',
    'Click to enlarge': '点击放大',
    'Enlarge QR code': '放大二维码',
    'Save QR code': '保存二维码',
    'Save it to your album and scan later': '保存到相册后再扫码',
    'WeChat ID': '微信号',
    'Demo payment (no real charge)': '演示支付（不会真实扣费）',
    'Demo mode: the payment is simulated and no real money is charged.':
      '演示模式：支付流程为模拟，不会产生真实扣费。',
    'Online payment is not available yet, please contact the administrator':
      '在线支付暂未开通，请联系管理员',
  },
  fr: {
    '+{{price}} / month': '+{{price}} / mois',
    '≈ {{amount}} / month': '≈ {{amount}} / mois',
    '5 Mbps dedicated': '5 Mbps dédiés',
    '10 Mbps dedicated': '10 Mbps dédiés',
    '20 Mbps dedicated': '20 Mbps dédiés',
    '50 Mbps dedicated': '50 Mbps dédiés',
    '100 Mbps dedicated': '100 Mbps dédiés',
    '6 months': '6 mois',
    '1 year': '1 an',
    '3 years': '3 ans',
    '99.9%': '99,9 %',
    '100 Mbps': '100 Mbps',
    'After the payment succeeds the server is provisioned automatically, and the IP address, username and password appear under My servers.':
      'Après le paiement, le serveur est provisionné automatiquement ; l’adresse IP, le nom d’utilisateur et le mot de passe apparaissent dans Mes serveurs.',
    'At least 8 characters': 'Au moins 8 caractères',
    Bandwidth: 'Bande passante',
    'Balanced compute and memory for most workloads':
      'Équilibre calcul et mémoire pour la plupart des usages',
    BGP: 'BGP',
    'Buy now · {{amount}}': 'Acheter · {{amount}}',
    'Can I change the password after the server is created?':
      'Puis-je changer le mot de passe après la création ?',
    'Can I switch between Guangzhou and Shanghai?':
      'Puis-je basculer entre Guangzhou et Shanghai ?',
    'Change password': 'Changer le mot de passe',
    'Change root password': 'Changer le mot de passe root',
    'Choose a family that matches your workload':
      'Choisissez une gamme adaptée à votre charge',
    'Cloud Servers': 'Serveurs cloud',
    'Compute Optimized': 'Optimisé calcul',
    'Configure a server': 'Configurer un serveur',
    'Configure a server and complete the payment. The instance details will show up here.':
      'Configurez un serveur et payez : les détails de l’instance s’afficheront ici.',
    'Configure server': 'Configurer le serveur',
    'Configure your server': 'Configurez votre serveur',
    'Confirm your order': 'Confirmer la commande',
    'Copy failed, please select and copy manually':
      'Copie impossible, sélectionnez et copiez manuellement',
    'Copy SSH command': 'Copier la commande SSH',
    'Dedicated GPU for inference and rendering':
      'GPU dédié pour inférence et rendu',
    'Dedicated inbound and outbound bandwidth':
      'Bande passante entrante et sortante dédiée',
    'DDoS protection included': 'Protection DDoS incluse',
    'East China hub with the strongest international routing':
      'Nœud de Chine de l’Est au meilleur routage international',
    'Encrypted checkout': 'Paiement chiffré',
    'Engineer support': 'Support ingénieur',
    'Entry-level instances for websites, blogs and small services. Instant provisioning with a public IPv4 address.':
      'Instances d’entrée de gamme pour sites, blogs et petits services. Provisionnement immédiat avec IPv4 publique.',
    'Everything you need to run production workloads, without the setup overhead.':
      'Tout ce qu’il faut pour la production, sans la préparation fastidieuse.',
    'Every instance comes with network-level traffic cleaning and a free firewall rule set.':
      'Chaque instance inclut un nettoyage de trafic réseau et un jeu de règles pare-feu gratuit.',
    'Expires on': 'Expire le',
    'Failed to update the password': 'Échec de la mise à jour du mot de passe',
    'From {{price}} / month': 'Dès {{price}} / mois',
    'Frequently asked questions': 'Questions fréquentes',
    'GPU Accelerated': 'Accéléré GPU',
    'GPU accelerated instances with dedicated graphics cards for model inference, rendering and video processing.':
      'Instances accélérées GPU avec cartes graphiques dédiées pour inférence, rendu et vidéo.',
    'General Purpose': 'Usage général',
    'General purpose instances for APIs, CI runners and container hosts. SSD storage and private networking included.':
      'Instances polyvalent pour API, runners CI et hôtes de conteneurs. Stockage SSD et réseau privé inclus.',
    Guangzhou: 'Guangzhou',
    'High frequency cores for heavy computation':
      'Ceurs haute fréquence pour le calcul intensif',
    'How long does it take to get my server?':
      'Combien de temps pour obtenir mon serveur ?',
    Included: 'Inclus',
    'Instant delivery': 'Livraison instantanée',
    'Instances are created automatically after the payment succeeds. Most orders are ready within five minutes, and the IP address, username and password appear under My servers as soon as provisioning finishes.':
      'Les instances sont créées automatiquement après le paiement. La plupart sont prêtes en cinq minutes ; IP, identifiant et mot de passe apparaissent dans Mes serveurs.',
    'Instances are provisioned automatically right after the payment is confirmed, usually within five minutes.':
      'Provisionnement automatique dès le paiement validé, généralement en moins de cinq minutes.',
    'Large memory pools for databases and caching':
      'Gros pools de mémoire pour bases de données et cache',
    Lite: 'Léger',
    'Memory Optimized': 'Optimisé mémoire',
    'Memory optimized instances for MySQL, PostgreSQL, Elasticsearch and in-memory caches. Large NVMe volumes.':
      'Instances optimisées mémoire pour MySQL, PostgreSQL, Elasticsearch et caches. Grands volumes NVMe.',
    'Most popular': 'Le plus populaire',
    'Multi-line network': 'Réseau multilingue',
    'My servers ({{count}})': 'Mes serveurs ({{count}})',
    'Network error, please try again later':
      'Erreur réseau, réessayez plus tard',
    New: 'Nouveau',
    'New: cloud servers are now available':
      'Nouveau : les serveurs cloud sont disponibles',
    'No servers yet': 'Aucun serveur pour l’instant',
    'Peak bandwidth': 'Bande passante crête',
    'Password must be at least 8 characters':
      'Le mot de passe doit comporter au moins 8 caractères',
    'Password updated': 'Mot de passe mis à jour',
    'Pay {{amount}}': 'Payer {{amount}}',
    'Payment failed, please try again': 'Paiement échoué, veuillez réessayer',
    'Payment successful': 'Paiement réussi',
    'Payment successful, your server is being provisioned':
      'Paiement réussi, votre serveur est en cours de création',
    'Pick a region, a plan, the specs and the bandwidth you need. Pay for 6 months, 1 year or 3 years, and get your server credentials right after the payment succeeds.':
      'Choisissez région, gamme, configuration et bande passante. Payez 6 mois, 1 an ou 3 ans et recevez vos identifiants dès le paiement validé.',
    'Please sign in before ordering': 'Connectez-vous avant de commander',
    'Prices shown include taxes and are settled in CNY.':
      'Prix TTC, réglés en CNY.',
    'Processing payment...': 'Traitement du paiement…',
    Provisioning: 'Provisionnement',
    'Public IP': 'IP publique',
    Region: 'Région',
    'Reliable cloud servers for your next project':
      'Des serveurs cloud fiables pour votre prochain projet',
    'Repeat the new password': 'Répétez le nouveau mot de passe',
    'Review the configuration and choose a payment method.':
      'Vérifiez la configuration et choisissez un moyen de paiement.',
    'SSH command copied': 'Commande SSH copiée',
    'Save 15%': '-15 %',
    'Save 28%': '-28 %',
    'Save {{amount}}': 'Économie {{amount}}',
    'Server: {{server}}': 'Serveur : {{server}}',
    'Sign in to order': 'Connectez-vous pour commander',
    'South China hub with direct access to mainland networks':
      'Nœud de Chine du Sud, accès direct aux réseaux continentaux',
    Specifications: 'Configuration',
    Term: 'Durée',
    'The two passwords do not match':
      'Les deux mots de passe ne correspondent pas',
    'The instance is kept for seven days after the expiry date so you can renew it. After that the data is released and the instance is reclaimed.':
      'L’instance est conservée sept jours après l’expiration pour permettre le renouvellement. Ensuite les données sont libérées et l’instance récupérée.',
    'Toggle password visibility': 'Afficher / masquer le mot de passe',
    'Total price': 'Prix total',
    'Uptime SLA': 'Uptime SLA',
    'Update password': 'Mettre à jour le mot de passe',
    'Upgrade any time': 'Évolvez à tout moment',
    'vCPU, memory and system disk': 'vCPU, mémoire et disque système',
    'View my servers': 'Voir mes serveurs',
    'What happens when the subscription expires?':
      'Que se passe-t-il à l’expiration ?',
    'Where the server is located': 'Emplacement du serveur',
    'Why choose our cloud servers': 'Pourquoi choisir nos serveurs cloud',
    'Yes. Both regions run the same images and the same plan catalogue, so you can move an instance by rebuilding it in the other region.':
      'Oui. Les deux régions utilisent les mêmes images et le même catalogue : migrez en reconstruisant l’instance dans l’autre région.',
    'Yes. Open My servers, find the instance and use the Change password action. The new password takes effect on the next login.':
      'Oui. Ouvrez Mes serveurs, sélectionnez l’instance et utilisez Changer le mot de passe. Le nouveau mot de passe s’applique à la prochaine connexion.',
    'You pay': 'Vous payez',
    'Your server is being provisioned. The IP address, username and password are available under My servers.':
      'Votre serveur est en cours de création. IP, identifiant et mot de passe sont disponibles dans Mes serveurs.',
    'Best for personal projects and blogs':
      'Idéal pour les projets personnels et les blogs',
    'Change plan, specs or bandwidth and the remaining period is charged at a prorated price.':
      'Modifier l’offre, les spécifications ou la bande passante : la période restante est facturée au prorata.',
    'Compute optimized instances for batch jobs, model inference and data processing. High clock speed vCPUs.':
      'Instances optimisées pour le calcul : tâches par lots, inférence de modèles et traitement de données. vCPU à haute fréquence.',
    Shanghai: 'Shanghai',
    'Talk to the engineers who run the platform instead of a generic ticket queue.':
      'Échangez avec les ingénieurs qui exploitent la plateforme au lieu d’attendre dans une file de tickets générique.',
    '{{days}} days left': '{{days}} jours restants',
    'Click to enlarge': 'Cliquez pour agrandir',
    'Enlarge QR code': 'Agrandir le code QR',
    'Save QR code': 'Enregistrer le code QR',
    'Save it to your album and scan later':
      'Enregistrez-le dans vos photos pour le scanner plus tard',
    'WeChat ID': 'Identifiant WeChat',
    'Demo payment (no real charge)':
      'Paiement de démonstration (aucun débit réel)',
    'Demo mode: the payment is simulated and no real money is charged.':
      'Mode démonstration : le paiement est simulé, aucun montant réel n’est débité.',
    'Online payment is not available yet, please contact the administrator':
      'Le paiement en ligne n’est pas encore disponible, veuillez contacter l’administrateur',
  },
  ja: {
    '+{{price}} / month': '+{{price}} / 月',
    '≈ {{amount}} / month': '約 {{amount}} / 月',
    '5 Mbps dedicated': '専用 5 Mbps',
    '10 Mbps dedicated': '専用 10 Mbps',
    '20 Mbps dedicated': '専用 20 Mbps',
    '50 Mbps dedicated': '専用 50 Mbps',
    '100 Mbps dedicated': '専用 100 Mbps',
    '6 months': '6 か月',
    '1 year': '1 年',
    '3 years': '3 年',
    '99.9%': '99.9%',
    '100 Mbps': '100 Mbps',
    'After the payment succeeds the server is provisioned automatically, and the IP address, username and password appear under My servers.':
      'お支払いが完了するとサーバーが自動開通し、IP アドレス・ユーザー名・パスワードは「マイサーバー」に表示されます。',
    'At least 8 characters': '8 文字以上',
    Bandwidth: '帯域幅',
    'Balanced compute and memory for most workloads':
      'ほとんどの業務に適した、性能とメモリのバランス',
    BGP: 'BGP',
    'Buy now · {{amount}}': '購入 · {{amount}}',
    'Can I change the password after the server is created?':
      '作成後にパスワードを変更できますか？',
    'Can I switch between Guangzhou and Shanghai?':
      '広州と上海を切り替えられますか？',
    'Change password': 'パスワード変更',
    'Change root password': 'root パスワードを変更',
    'Choose a family that matches your workload':
      '用途に合うファミリーを選択してください',
    'Cloud Servers': 'クラウドサーバー',
    'Compute Optimized': '計算最適化型',
    'Configure a server': 'サーバーを設定する',
    'Configure a server and complete the payment. The instance details will show up here.':
      'サーバーを設定して支払いすると、インスタンス情報がここに表示されます。',
    'Configure server': 'サーバー設定',
    'Configure your server': 'サーバーを設定する',
    'Confirm your order': '注文の確認',
    'Copy failed, please select and copy manually':
      'コピーに失敗しました。手動で選択してコピーしてください',
    'Copy SSH command': 'SSH コマンドをコピー',
    'Dedicated GPU for inference and rendering':
      '推論とレンダリングに向けた専用 GPU',
    'Dedicated inbound and outbound bandwidth': '上り・下りとも専用帯域',
    'DDoS protection included': 'DDoS 防御標準搭載',
    'East China hub with the strongest international routing':
      '国際回線の最も安定した中国東部ハブ',
    'Encrypted checkout': '暗号化決済',
    'Engineer support': 'エンジニアサポート',
    'Entry-level instances for websites, blogs and small services. Instant provisioning with a public IPv4 address.':
      'ウェブサイト・ブログ・小規模サービス向けのエントリーモデル。即座に固定 IP を割り当て。',
    'Everything you need to run production workloads, without the setup overhead.':
      '本番運用に必要な機能を、繁琐なセットアップなしで。',
    'Every instance comes with network-level traffic cleaning and a free firewall rule set.':
      '全インスタンスにネットワークレベルのトラフィッククリーニングと無料のファイアウォールルールが付属。',
    'Expires on': '有効期限',
    'Failed to update the password': 'パスワードの更新に失敗しました',
    'From {{price}} / month': '{{price}} / 月から',
    'Frequently asked questions': 'よくある質問',
    'GPU Accelerated': 'GPU アクセラレーション',
    'GPU accelerated instances with dedicated graphics cards for model inference, rendering and video processing.':
      '専用グラフィックカード装载の推論・レンダリング・動画処理向けインスタンス。',
    'General Purpose': '汎用型',
    'General purpose instances for APIs, CI runners and container hosts. SSD storage and private networking included.':
      'API・CI ランナー・コンテナホスト向けの汎用インスタンス。SSD とプライベートネットワーク標準搭載。',
    Guangzhou: '広州',
    'High frequency cores for heavy computation':
      '高クロックコアによるヘビー計算向け',
    'How long does it take to get my server?':
      'サーバーはどのくらいで届きますか？',
    Included: '込み',
    'Instant delivery': '即時デリバリー',
    'Instances are created automatically after the payment succeeds. Most orders are ready within five minutes, and the IP address, username and password appear under My servers as soon as provisioning finishes.':
      '支払い完了後にインスタンスは自動作成されます。ほとんどの場合は 5 分以内に準備が整い、IP・ユーザー名・パスワードが「マイサーバー」に表示されます。',
    'Instances are provisioned automatically right after the payment is confirmed, usually within five minutes.':
      '支払い確認後まもなく自動開通し、通常 5 分以内に開通します。',
    'Large memory pools for databases and caching':
      'データベースとキャッシュの大きなメモリプール',
    Lite: '軽量型',
    'Memory Optimized': 'メモリ最適化型',
    'Memory optimized instances for MySQL, PostgreSQL, Elasticsearch and in-memory caches. Large NVMe volumes.':
      'MySQL・PostgreSQL・Elasticsearch・インメモリキャッシュ向けのメモリ最適化インスタンス。大容量 NVMe 搭載。',
    'Most popular': '人気 No.1',
    'Multi-line network': 'マルチラインネットワーク',
    'My servers ({{count}})': 'マイサーバー（{{count}}）',
    'Network error, please try again later':
      'ネットワークエラーです。後ほど再試行してください',
    New: '新登場',
    'New: cloud servers are now available':
      '新登場：クラウドサーバーの販売を開始しました',
    'No servers yet': 'サーバーはまだありません',
    'Peak bandwidth': '最大帯域',
    'Password must be at least 8 characters':
      'パスワードは少なくとも8文字である必要があります',
    'Password updated': 'パスワードを更新しました',
    'Pay {{amount}}': '{{amount}} を支払う',
    'Payment failed, please try again':
      '支払いに失敗しました。もう一度お試しください',
    'Payment successful': '支払いが完了しました',
    'Payment successful, your server is being provisioned':
      '支払い完了。サーバーを開通中です',
    'Pick a region, a plan, the specs and the bandwidth you need. Pay for 6 months, 1 year or 3 years, and get your server credentials right after the payment succeeds.':
      'リージョン・型号・仕様・帯域幅を選択。6 か月・1 年・3 年のいずれかで支払い、完了後すぐにサーバー情報が发放されます。',
    'Please sign in before ordering': 'ご注文の前にログインしてください',
    'Prices shown include taxes and are settled in CNY.':
      '表示価格は税込、人民币での決済です。',
    'Processing payment...': '支払いを処理しています…',
    Provisioning: '開通時間',
    'Public IP': 'グローバル IP',
    Region: 'リージョン',
    'Reliable cloud servers for your next project':
      '次のプロジェクトのための、信頼できるクラウドサーバー',
    'Repeat the new password': '新しいパスワードを再入力',
    'Review the configuration and choose a payment method.':
      '設定内容を確認し、支払い方法を選んでください。',
    'SSH command copied': 'SSH コマンドをコピーしました',
    'Save 15%': '15% おトク',
    'Save 28%': '28% おトク',
    'Save {{amount}}': '{{amount}} おトク',
    'Server: {{server}}': 'サーバー: {{server}}',
    'Sign in to order': 'ログインして注文',
    'South China hub with direct access to mainland networks':
      '中国大陸ネットワークへ直接接続できる華南ハブ',
    Specifications: '仕様',
    Term: '契約期間',
    'The two passwords do not match': 'パスワードが一致しません',
    'The instance is kept for seven days after the expiry date so you can renew it. After that the data is released and the instance is reclaimed.':
      '期限切れ後も 7 日間インスタンスを保持し、更新できます。その後はデータが解放され、インスタンスも回収されます。',
    'Toggle password visibility': 'パスワードの表示 / 非表示',
    'Total price': '合計金額',
    'Uptime SLA': '稼働時間SLA',
    'Update password': 'パスワードを更新',
    'Upgrade any time': 'いつでも変更可能',
    'vCPU, memory and system disk': 'vCPU・メモリ・システムディスク',
    'View my servers': 'マイサーバーを表示',
    'What happens when the subscription expires?':
      '契約期限が切れるとどうなりますか？',
    'Where the server is located': 'サーバーの所在リージョン',
    'Why choose our cloud servers': 'なぜ私たちのクラウドサーバーなのか',
    'Yes. Both regions run the same images and the same plan catalogue, so you can move an instance by rebuilding it in the other region.':
      'はい。両リージョンで同じイメージと同じカタログを提供しているので、もう一方のリージョンで再作成すれば移行できます。',
    'Yes. Open My servers, find the instance and use the Change password action. The new password takes effect on the next login.':
      'はい。「マイサーバー」で対象インスタンスを選び「パスワード変更」を実行してください。新しいパスワードは次回ログイン時に有効になります。',
    'You pay': 'お支払い金額',
    'Your server is being provisioned. The IP address, username and password are available under My servers.':
      'サーバーを開通中です。IP・ユーザー名・パスワードは「マイサーバー」で確認できます。',
    'Best for personal projects and blogs':
      '個人プロジェクトやブログに最適です',
    'Change plan, specs or bandwidth and the remaining period is charged at a prorated price.':
      'プラン・仕様・帯域を変更すると、残り期間は按分精算されます。',
    'Compute optimized instances for batch jobs, model inference and data processing. High clock speed vCPUs.':
      '計算最適化インスタンス。バッチ処理、モデル推論、データ処理に適しています。高クロック速度の vCPU を搭載。',
    Shanghai: '上海',
    'Talk to the engineers who run the platform instead of a generic ticket queue.':
      '汎用チケットの待ち行列ではなく、プラットフォームを運用するエンジニアと直接話せます。',
    '{{days}} days left': '残り {{days}} 日',
    'Click to enlarge': 'クリックして拡大',
    'Enlarge QR code': 'QRコードを拡大',
    'Save QR code': 'QRコードを保存',
    'Save it to your album and scan later': 'アルバムに保存して後からスキャン',
    'WeChat ID': 'WeChatのアカウントID',
    'Demo payment (no real charge)': 'デモ決済（実際の請求は発生しません）',
    'Demo mode: the payment is simulated and no real money is charged.':
      'デモモード：決済はシミュレーションであり、実際の請求は発生しません。',
    'Online payment is not available yet, please contact the administrator':
      'オンライン決済はまだ利用できません。管理者までお問い合わせください。',
  },
  ru: {
    '+{{price}} / month': '+{{price}} / мес.',
    '≈ {{amount}} / month': '≈ {{amount}} / мес.',
    '5 Mbps dedicated': 'Выделенные 5 Мбит/с',
    '10 Mbps dedicated': 'Выделенные 10 Мбит/с',
    '20 Mbps dedicated': 'Выделенные 20 Мбит/с',
    '50 Mbps dedicated': 'Выделенные 50 Мбит/с',
    '100 Mbps dedicated': 'Выделенные 100 Мбит/с',
    '6 months': '6 месяцев',
    '1 year': '1 год',
    '3 years': '3 года',
    '99.9%': '99,9 %',
    '100 Mbps': '100 Мбит/с',
    'After the payment succeeds the server is provisioned automatically, and the IP address, username and password appear under My servers.':
      'После оплаты сервер создаётся автоматически, а IP-адрес, логин и пароль появляются в разделе «Мои серверы».',
    'At least 8 characters': 'Не менее 8 символов',
    Bandwidth: 'Канал',
    'Balanced compute and memory for most workloads':
      'Оптимальный баланс CPU и памяти для большинства задач',
    BGP: 'BGP',
    'Buy now · {{amount}}': 'Купить · {{amount}}',
    'Can I change the password after the server is created?':
      'Можно ли сменить пароль после создания сервера?',
    'Can I switch between Guangzhou and Shanghai?':
      'Можно ли переключаться между Гуанчжоу и Шанхаем?',
    'Change password': 'Сменить пароль',
    'Change root password': 'Сменить пароль root',
    'Choose a family that matches your workload':
      'Выберите линейку под вашу нагрузку',
    'Cloud Servers': 'Облачные серверы',
    'Compute Optimized': 'Вычислительный',
    'Configure a server': 'Настроить сервер',
    'Configure a server and complete the payment. The instance details will show up here.':
      'Настройте сервер и оплатите — данные сервера появятся здесь.',
    'Configure server': 'Настройка сервера',
    'Configure your server': 'Настройте свой сервер',
    'Confirm your order': 'Подтверждение заказа',
    'Copy failed, please select and copy manually':
      'Не удалось скопировать, выделите и скопируйте вручную',
    'Copy SSH command': 'Скопировать SSH-команду',
    'Dedicated GPU for inference and rendering':
      'Выделенный GPU для инференса и рендеринга',
    'Dedicated inbound and outbound bandwidth':
      'Выделенный входящий и исходящий канал',
    'DDoS protection included': 'Защита от DDoS включена',
    'East China hub with the strongest international routing':
      'Хаб в Восточном Китае с лучшей международной маршрутизацией',
    'Encrypted checkout': 'Шифрованная оплата',
    'Engineer support': 'Поддержка инженеров',
    'Entry-level instances for websites, blogs and small services. Instant provisioning with a public IPv4 address.':
      'Начальные серверы для сайтов, блогов и небольших сервисов. Мгновенная выдача с публичным IPv4.',
    'Everything you need to run production workloads, without the setup overhead.':
      'Всё для production-нагрузок — без долгой подготовки.',
    'Every instance comes with network-level traffic cleaning and a free firewall rule set.':
      'Каждый сервер включает сетевую очистку трафика и бесплатный набор правил фаервола.',
    'Expires on': 'Истекает',
    'Failed to update the password': 'Не удалось обновить пароль',
    'From {{price}} / month': 'От {{price}} / мес.',
    'Frequently asked questions': 'Частые вопросы',
    'GPU Accelerated': 'С GPU',
    'GPU accelerated instances with dedicated graphics cards for model inference, rendering and video processing.':
      'Серверы с выделенными видеокартами для инференса, рендеринга и видеообработки.',
    'General Purpose': 'Универсальный',
    'General purpose instances for APIs, CI runners and container hosts. SSD storage and private networking included.':
      'Универсальные серверы для API, CI-раннеров и контейнерных хостов. SSD и приватная сеть включены.',
    Guangzhou: 'Гуанчжоу',
    'High frequency cores for heavy computation':
      'Высокочастотные ядра для тяжёлых вычислений',
    'How long does it take to get my server?': 'Как быстро я получу сервер?',
    Included: 'Включено',
    'Instant delivery': 'Мгновенная выдача',
    'Instances are created automatically after the payment succeeds. Most orders are ready within five minutes, and the IP address, username and password appear under My servers as soon as provisioning finishes.':
      'Серверы создаются автоматически после оплаты. Большинство заказов готовы за пять минут, а IP, логин и пароль появляются в «Моих серверах».',
    'Instances are provisioned automatically right after the payment is confirmed, usually within five minutes.':
      'Автоматическая выдача сразу после подтверждения оплаты, обычно за пять минут.',
    'Large memory pools for databases and caching':
      'Большие объёмы памяти для баз данных и кэша',
    Lite: 'Лёгкий',
    'Memory Optimized': 'С большим объёмом памяти',
    'Memory optimized instances for MySQL, PostgreSQL, Elasticsearch and in-memory caches. Large NVMe volumes.':
      'Серверы для MySQL, PostgreSQL, Elasticsearch и кэшей в памяти. Большие тома NVMe.',
    'Most popular': 'Популярный',
    'Multi-line network': 'Мультилинейная сеть',
    'My servers ({{count}})': 'Мои серверы ({{count}})',
    'Network error, please try again later':
      'Ошибка сети, повторите попытку позже',
    New: 'Новинка',
    'New: cloud servers are now available':
      'Новинка: облачные серверы уже доступны',
    'No servers yet': 'Серверов пока нет',
    'Peak bandwidth': 'Пиковый канал',
    'Password must be at least 8 characters':
      'Пароль должен содержать не менее 8 символов',
    'Password updated': 'Пароль обновлён',
    'Pay {{amount}}': 'Оплатить {{amount}}',
    'Payment failed, please try again': 'Оплата не удалась, попробуйте ещё раз',
    'Payment successful': 'Оплата прошла',
    'Payment successful, your server is being provisioned':
      'Оплата прошла, сервер создаётся',
    'Pick a region, a plan, the specs and the bandwidth you need. Pay for 6 months, 1 year or 3 years, and get your server credentials right after the payment succeeds.':
      'Выберите регион, линейку, конфигурацию и канал. Оплатите 6 месяцев, год или 3 года и сразу получите доступы к серверу.',
    'Please sign in before ordering': 'Войдите перед оформлением заказа',
    'Prices shown include taxes and are settled in CNY.':
      'Цены указаны с налогами, оплата в юанях.',
    'Processing payment...': 'Обрабатываем оплату…',
    Provisioning: 'Время выдачи',
    'Public IP': 'Публичный IP',
    Region: 'Регион',
    'Reliable cloud servers for your next project':
      'Надёжные облачные серверы для вашего следующего проекта',
    'Repeat the new password': 'Повторите новый пароль',
    'Review the configuration and choose a payment method.':
      'Проверьте конфигурацию и выберите способ оплаты.',
    'SSH command copied': 'SSH-команда скопирована',
    'Save 15%': 'Экономия 15%',
    'Save 28%': 'Экономия 28%',
    'Save {{amount}}': 'Экономия {{amount}}',
    'Server: {{server}}': 'Сервер: {{server}}',
    'Sign in to order': 'Войти и заказать',
    'South China hub with direct access to mainland networks':
      'Хаб на юге Китая с прямым доступом к магистральным сетям',
    Specifications: 'Конфигурация',
    Term: 'Срок',
    'The two passwords do not match': 'Пароли не совпадают',
    'The instance is kept for seven days after the expiry date so you can renew it. After that the data is released and the instance is reclaimed.':
      'После окончания срока сервер хранится ещё 7 дней для продления. Затем данные удаляются, а сервер возвращается в пул.',
    'Toggle password visibility': 'Показать или скрыть пароль',
    'Total price': 'Итого',
    'Uptime SLA': 'SLA доступности',
    'Update password': 'Обновить пароль',
    'Upgrade any time': 'Изменение в любой момент',
    'vCPU, memory and system disk': 'vCPU, память и системный диск',
    'View my servers': 'Мои серверы',
    'What happens when the subscription expires?':
      'Что происходит после окончания срока?',
    'Where the server is located': 'Где расположен сервер',
    'Why choose our cloud servers': 'Почему выбирают наши серверы',
    'Yes. Both regions run the same images and the same plan catalogue, so you can move an instance by rebuilding it in the other region.':
      'Да. В обоих регионах одинаковые образы и каталог линеек: перенесите сервер, пересоздав его в другом регионе.',
    'Yes. Open My servers, find the instance and use the Change password action. The new password takes effect on the next login.':
      'Да. Откройте «Мои серверы», выберите сервер и нажмите «Сменить пароль». Новый пароль действует со следующего входа.',
    'You pay': 'К оплате',
    'Your server is being provisioned. The IP address, username and password are available under My servers.':
      'Сервер создаётся. IP, логин и пароль доступны в разделе «Мои серверы».',
    'Best for personal projects and blogs':
      'Идеально для личных проектов и блогов',
    'Change plan, specs or bandwidth and the remaining period is charged at a prorated price.':
      'При смене тарифа, конфигурации или полосы пропорционально оплачивается остаток периода.',
    'Compute optimized instances for batch jobs, model inference and data processing. High clock speed vCPUs.':
      'Инстансы, оптимизированные для вычислений: пакетные задачи, инференс моделей и обработка данных. Высокочастотные vCPU.',
    Shanghai: 'Шанхай',
    'Talk to the engineers who run the platform instead of a generic ticket queue.':
      'Общайтесь напрямую с инженерами, которые ведут платформу, а не в общей очереди тикетов.',
    '{{days}} days left': 'Осталось {{days}} дн.',
    'Click to enlarge': 'Нажмите, чтобы увеличить',
    'Enlarge QR code': 'Увеличить QR-код',
    'Save QR code': 'Сохранить QR-код',
    'Save it to your album and scan later':
      'Сохраните в галерею, чтобы отсканировать позже',
    'WeChat ID': 'Идентификатор WeChat',
    'Demo payment (no real charge)':
      'Демонстрационная оплата (без реального списания)',
    'Demo mode: the payment is simulated and no real money is charged.':
      'Демо-режим: платёж имитируется, реальное списание средств не происходит.',
    'Online payment is not available yet, please contact the administrator':
      'Онлайн-оплата пока недоступна, пожалуйста, свяжитесь с администратором',
  },
  vi: {
    '+{{price}} / month': '+{{price}} / tháng',
    '≈ {{amount}} / month': '≈ {{amount}} / tháng',
    '5 Mbps dedicated': 'Dành riêng 5 Mbps',
    '10 Mbps dedicated': 'Dành riêng 10 Mbps',
    '20 Mbps dedicated': 'Dành riêng 20 Mbps',
    '50 Mbps dedicated': 'Dành riêng 50 Mbps',
    '100 Mbps dedicated': 'Dành riêng 100 Mbps',
    '6 months': '6 tháng',
    '1 year': '1 năm',
    '3 years': '3 năm',
    '99.9%': '99,9%',
    '100 Mbps': '100 Mbps',
    'After the payment succeeds the server is provisioned automatically, and the IP address, username and password appear under My servers.':
      'Sau khi thanh toán thành công, máy chủ được cấp tự động; địa chỉ IP, tên đăng nhập và mật khẩu sẽ hiển thị trong Máy chủ của tôi.',
    'At least 8 characters': 'Ít nhất 8 ký tự',
    Bandwidth: 'Băng thông',
    'Balanced compute and memory for most workloads':
      'Cân bằng CPU và RAM cho hầu hết tác vụ',
    BGP: 'BGP',
    'Buy now · {{amount}}': 'Mua ngay · {{amount}}',
    'Can I change the password after the server is created?':
      'Tôi có thể đổi mật khẩu sau khi tạo máy chủ không?',
    'Can I switch between Guangzhou and Shanghai?':
      'Tôi có thể chuyển giữa Quảng Châu và Thượng Hải không?',
    'Change password': 'Đổi mật khẩu',
    'Change root password': 'Đổi mật khẩu root',
    'Choose a family that matches your workload':
      'Chọn dòng máy phù hợp với tải của bạn',
    'Cloud Servers': 'Máy chủ đám mây',
    'Compute Optimized': 'Tối ưu tính toán',
    'Configure a server': 'Cấu hình máy chủ',
    'Configure a server and complete the payment. The instance details will show up here.':
      'Cấu hình máy chủ và thanh toán. Thông tin máy chủ sẽ hiển thị tại đây.',
    'Configure server': 'Cấu hình máy chủ',
    'Configure your server': 'Cấu hình máy chủ của bạn',
    'Confirm your order': 'Xác nhận đơn hàng',
    'Copy failed, please select and copy manually':
      'Sao chép thất bại, vui lòng chọn và sao chép thủ công',
    'Copy SSH command': 'Sao chép lệnh SSH',
    'Dedicated GPU for inference and rendering':
      'GPU riêng cho suy luận và kết xuất',
    'Dedicated inbound and outbound bandwidth':
      'Băng thông chiều ra và chiều vào riêng',
    'DDoS protection included': 'Kèm chống DDoS',
    'East China hub with the strongest international routing':
      'Trung tâm miền Đông với đường quốc tế tốt nhất',
    'Encrypted checkout': 'Thanh toán mã hóa',
    'Engineer support': 'Hỗ trợ bởi kỹ sư',
    'Entry-level instances for websites, blogs and small services. Instant provisioning with a public IPv4 address.':
      'Máy chủ phổ thông cho website, blog và dịch vụ nhỏ. Cấp phát tức thì kèm IPv4 công cộng.',
    'Everything you need to run production workloads, without the setup overhead.':
      'Mọi thứ cần cho vận hành thực tế, không tốn công chuẩn bị.',
    'Every instance comes with network-level traffic cleaning and a free firewall rule set.':
      'Mỗi máy chủ đi kèm lọc lưu lượng mạng và bộ quy tắc tường lửa miễn phí.',
    'Expires on': 'Hết hạn',
    'Failed to update the password': 'Không đổi được mật khẩu',
    'From {{price}} / month': 'Từ {{price}} / tháng',
    'Frequently asked questions': 'Câu hỏi thường gặp',
    'GPU Accelerated': 'Tăng tốc GPU',
    'GPU accelerated instances with dedicated graphics cards for model inference, rendering and video processing.':
      'Máy chủ tăng tốc GPU với card đồ họa riêng cho suy luận, kết xuất và xử lý video.',
    'General Purpose': 'Đa năng',
    'General purpose instances for APIs, CI runners and container hosts. SSD storage and private networking included.':
      'Máy chủ đa năng cho API, CI runner và host container. Đi kèm lưu trữ SSD và mạng riêng.',
    Guangzhou: 'Quảng Châu',
    'High frequency cores for heavy computation':
      'Nhân xung cao cho tác vụ tính toán nặng',
    'How long does it take to get my server?':
      'Mất bao lâu để nhận được máy chủ?',
    Included: 'Đã bao gồm',
    'Instant delivery': 'Bàn giao tức thì',
    'Instances are created automatically after the payment succeeds. Most orders are ready within five minutes, and the IP address, username and password appear under My servers as soon as provisioning finishes.':
      'Máy chủ được tạo tự động sau khi thanh toán. Hầu hết đơn hàng sẵn sàng trong năm phút, IP, tên đăng nhập và mật khẩu hiển thị trong Máy chủ của tôi.',
    'Instances are provisioned automatically right after the payment is confirmed, usually within five minutes.':
      'Cấp phát tự động ngay khi xác nhận thanh toán, thường trong năm phút.',
    'Large memory pools for databases and caching':
      'Lượng RAM lớn cho cơ sở dữ liệu và bộ nhớ đệm',
    Lite: 'Nhẹ',
    'Memory Optimized': 'Tối ưu RAM',
    'Memory optimized instances for MySQL, PostgreSQL, Elasticsearch and in-memory caches. Large NVMe volumes.':
      'Máy chủ tối ưu RAM cho MySQL, PostgreSQL, Elasticsearch và cache trong RAM. Dung lượng NVMe lớn.',
    'Most popular': 'Phổ biến nhất',
    'Multi-line network': 'Mạng đa tuyến',
    'My servers ({{count}})': 'Máy chủ của tôi ({{count}})',
    'Network error, please try again later': 'Lỗi mạng, vui lòng thử lại sau',
    New: 'Mới',
    'New: cloud servers are now available': 'Mới: đã mở bán máy chủ đám mây',
    'No servers yet': 'Chưa có máy chủ nào',
    'Peak bandwidth': 'Băng thông đỉnh',
    'Password must be at least 8 characters':
      'Mật khẩu phải có ít nhất 8 ký tự',
    'Password updated': 'Đã cập nhật mật khẩu',
    'Pay {{amount}}': 'Thanh toán {{amount}}',
    'Payment failed, please try again': 'Thanh toán thất bại, vui lòng thử lại',
    'Payment successful': 'Thanh toán thành công',
    'Payment successful, your server is being provisioned':
      'Thanh toán thành công, máy chủ đang được cấp phát',
    'Pick a region, a plan, the specs and the bandwidth you need. Pay for 6 months, 1 year or 3 years, and get your server credentials right after the payment succeeds.':
      'Chọn khu vực, dòng máy, cấu hình và băng thông. Trả 6 tháng, 1 năm hoặc 3 năm và nhận thông tin đăng nhập ngay sau khi thanh toán.',
    'Please sign in before ordering': 'Vui lòng đăng nhập trước khi đặt hàng',
    'Prices shown include taxes and are settled in CNY.':
      'Giá đã gồm thuế và thanh toán bằng CNY.',
    'Processing payment...': 'Đang xử lý thanh toán…',
    Provisioning: 'Thời gian cấp phát',
    'Public IP': 'IP công cộng',
    Region: 'Khu vực',
    'Reliable cloud servers for your next project':
      'Máy chủ đám mây ổn định cho dự án tiếp theo của bạn',
    'Repeat the new password': 'Nhập lại mật khẩu mới',
    'Review the configuration and choose a payment method.':
      'Kiểm tra cấu hình và chọn phương thức thanh toán.',
    'SSH command copied': 'Đã sao chép lệnh SSH',
    'Save 15%': 'Tiết kiệm 15%',
    'Save 28%': 'Tiết kiệm 28%',
    'Save {{amount}}': 'Tiết kiệm {{amount}}',
    'Server: {{server}}': 'Máy chủ: {{server}}',
    'Sign in to order': 'Đăng nhập để đặt hàng',
    'South China hub with direct access to mainland networks':
      'Trung tâm miền Nam, kết nối trực tiếp mạng lõi trong nước',
    Specifications: 'Cấu hình',
    Term: 'Thời hạn',
    'The two passwords do not match': 'Hai mật khẩu không khớp',
    'The instance is kept for seven days after the expiry date so you can renew it. After that the data is released and the instance is reclaimed.':
      'Máy chủ được giữ thêm 7 ngày sau khi hết hạn để gia hạn. Sau đó dữ liệu bị thu hồi và máy chủ được tái cấp phối.',
    'Toggle password visibility': 'Hiện / ẩn mật khẩu',
    'Total price': 'Tổng cộng',
    'Uptime SLA': 'Uptime SLA',
    'Update password': 'Cập nhật mật khẩu',
    'Upgrade any time': 'Nâng cấp bất cứ lúc nào',
    'vCPU, memory and system disk': 'vCPU, RAM và ổ đĩa hệ thống',
    'View my servers': 'Xem máy chủ của tôi',
    'What happens when the subscription expires?':
      'Điều gì xảy ra khi thuê bao hết hạn?',
    'Where the server is located': 'Vị trí đặt máy chủ',
    'Why choose our cloud servers': 'Vì sao chọn máy chủ đám mây của chúng tôi',
    'Yes. Both regions run the same images and the same plan catalogue, so you can move an instance by rebuilding it in the other region.':
      'Có. Cả hai khu vực dùng chung image và danh mục dòng máy, nên bạn có thể chuyển bằng cách dựng lại ở khu vực còn lại.',
    'Yes. Open My servers, find the instance and use the Change password action. The new password takes effect on the next login.':
      'Có. Mở Máy chủ của tôi, chọn máy chủ và dùng thao tác Đổi mật khẩu. Mật khẩu mới có hiệu lực từ lần đăng nhập sau.',
    'You pay': 'Bạn trả',
    'Your server is being provisioned. The IP address, username and password are available under My servers.':
      'Máy chủ đang được cấp phát. IP, tên đăng nhập và mật khẩu có trong Máy chủ của tôi.',
    'Best for personal projects and blogs': 'Phù hợp với dự án cá nhân và blog',
    'Change plan, specs or bandwidth and the remaining period is charged at a prorated price.':
      'Khi đổi gói, cấu hình hoặc băng thông, thời gian còn lại được tính theo tỷ lệ.',
    'Compute optimized instances for batch jobs, model inference and data processing. High clock speed vCPUs.':
      'Máy chủ tối ưu cho tính toán: xử lý hàng loạt, suy luận mô hình và xử lý dữ liệu. vCPU tần số cao.',
    Shanghai: 'Thượng Hải',
    'Talk to the engineers who run the platform instead of a generic ticket queue.':
      'Nói chuyện trực tiếp với các kỹ sư vận hành nền tảng thay vì xếp hàng trong hàng đợi ticket chung.',
    '{{days}} days left': 'Còn {{days}} ngày',
    'Click to enlarge': 'Nhấn để phóng to',
    'Enlarge QR code': 'Phóng to mã QR',
    'Save QR code': 'Lưu mã QR',
    'Save it to your album and scan later': 'Lưu vào thư viện rồi quét sau',
    'WeChat ID': 'Tài khoản WeChat',
    'Demo payment (no real charge)':
      'Thanh toán thử nghiệm (không bị trừ tiền thật)',
    'Demo mode: the payment is simulated and no real money is charged.':
      'Chế độ thử nghiệm: thanh toán được mô phỏng, không có khoản tiền nào bị trừ thật.',
    'Online payment is not available yet, please contact the administrator':
      'Thanh toán trực tuyến chưa mở, vui lòng liên hệ quản trị viên',
  },
}

async function main() {
  let totalAdded = 0
  const conflicts = []

  for (const [locale, trans] of Object.entries(newKeys)) {
    const filePath = path.join(LOCALES_DIR, `${locale}.json`)
    const json = JSON.parse(await fs.readFile(filePath, 'utf8'))

    let count = 0
    for (const [key, value] of Object.entries(trans)) {
      if (!Object.prototype.hasOwnProperty.call(json.translation, key)) {
        json.translation[key] = value
        count++
      } else if (json.translation[key] !== value) {
        // Never silently rewrite an existing translation: report and skip.
        conflicts.push({
          locale,
          key,
          existing: json.translation[key],
          incoming: value,
        })
      }
    }

    if (count > 0) {
      // Keep the original key order; `bun run i18n:sync` re-normalizes the order.
      await fs.writeFile(filePath, stableStringify(json), 'utf8')
    }

    console.log(`${locale}: ${count} translations applied`)
    totalAdded += count
  }

  if (conflicts.length > 0) {
    console.log(
      `\n${conflicts.length} conflict(s) skipped (existing translation kept):`
    )
    for (const c of conflicts) {
      console.log(`  [${c.locale}] "${c.key}"`)
      console.log(`      existing : ${c.existing}`)
      console.log(`      incoming : ${c.incoming}`)
    }
  }

  console.log(`\nTotal: ${totalAdded} translations applied`)
}

main().catch((err) => {
  console.error(err)
  process.exitCode = 1
})
