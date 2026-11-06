/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

/**
 * 客服微信的共享资源。
 *
 * 首页悬浮客服按钮与服务器租赁的「联系管理员」弹窗共用同一份二维码。
 * 换码时只需替换 public/ 下的两个文件；如果两处各写一份路径，
 * 迟早会出现「首页是新码、购买页还是旧码」的事故。
 *
 * SVG 是矢量版（放大不糊），PNG 供移动端长按保存 / 下载使用。
 */
export const CUSTOMER_SERVICE_QR_SVG = '/customer-service-wechat-qr.svg'
export const CUSTOMER_SERVICE_QR_PNG = '/customer-service-wechat-qr.png'

/** 管理员微信号，展示在二维码下方供无法扫码的用户手动搜索 */
export const CUSTOMER_SERVICE_WECHAT_ID = 'heibaidao'
