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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  formatVpnMoney,
  formatVpnTime,
  vpnPageRange,
  vpnProviderLabel,
  vpnStatusLabelKey,
} from './format'

describe('VPN admin order formatting', () => {
  test('keeps decimal string amounts exactly as returned by the server', () => {
    assert.equal(formatVpnMoney('597.00'), '¥597.00')
    assert.equal(formatVpnMoney('0.02'), '¥0.02')
    assert.equal(formatVpnMoney(199), '¥199.00')
  })

  test('unpaid orders have no paid timestamp', () => {
    assert.equal(formatVpnTime(0), '-')
    assert.equal(formatVpnTime(-1), '-')
    assert.notEqual(formatVpnTime(1762700000), '-')
  })

  test('status maps to i18n keys', () => {
    assert.equal(vpnStatusLabelKey('success'), 'Success')
    assert.equal(vpnStatusLabelKey('pending'), 'Pending')
    assert.equal(vpnStatusLabelKey('failed'), 'Failed')
    assert.equal(vpnStatusLabelKey('expired'), 'Expired')
  })

  test('distinguishes direct alipay from aggregator epay', () => {
    assert.equal(vpnProviderLabel('alipay'), 'Alipay (direct)')
    assert.equal(vpnProviderLabel('epay'), 'Epay')
    assert.equal(vpnProviderLabel('unknown'), 'Unknown')
    assert.equal(vpnProviderLabel(''), 'Unknown')
  })

  test('page range never exceeds total', () => {
    assert.deepEqual(vpnPageRange(1, 10, 8), [1, 8])
    assert.deepEqual(vpnPageRange(2, 10, 25), [11, 20])
    assert.deepEqual(vpnPageRange(3, 10, 25), [21, 25])
    assert.deepEqual(vpnPageRange(1, 10, 0), [0, 0])
  })
})
