/*
Copyright (C) 2025 QuantumNous

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

export function getAutoDLDescription(modelName, t) {
  switch (modelName) {
    case 'autodl:minimax-h3-text-to-video':
      return t(
        'Text-to-video, no reference image needed. Duration 1-15s; 480p/768p in vertical, horizontal and 1:1. Billed per second.',
      );
    case 'autodl:minimax-h3-lightx2v-v5':
      return t(
        'Multi-reference video, 1-9 reference images required. Duration 1-10s; 480p/768p in vertical, horizontal and 1:1. Supports seed. Billed per second.',
      );
    case 'autodl:minimax-h3-lightx2v-v5-15s':
      return t(
        'Multi-reference video, 15s version, 1-9 reference images required. Duration 1-15s; 480p/768p in vertical, horizontal and 1:1. Supports seed. Billed per second.',
      );
    case 'autodl:minimax-h3-b99-12s':
      return t(
        'Multi-reference video, 12s version, 1-9 reference images required. Duration 1-12s; 736p only in vertical, horizontal and 1:1. Supports seed. Billed per second.',
      );
    case 'autodl:minimax-h3-u24':
      return t(
        'Multi-reference video with audio, quality-priority variant. Requires 1-9 images and accepts up to 3 audio tracks; duration 1-15s; 480p/768p in vertical, horizontal and 1:1; seed accepts 0.',
      );
    case 'autodl:minimax-h3-u08':
      return t(
        'Multi-reference video with audio, speed-priority variant. Requires 1-9 images and accepts up to 3 audio tracks; duration 1-15s; 480p/768p in vertical, horizontal and 1:1; seed accepts 0.',
      );
    case 'autodl:minimax-h3-image-audio-10s':
      return t(
        'Multi-reference video with audio, 10-second variant. Images and audio are optional; up to 9 images and 3 audio tracks; duration 1-10s; 480p/768p/1080p in vertical and horizontal.',
      );
    case 'autodl:minimax-h3-image-audio-15s':
      return t(
        'Multi-reference video with audio, 15-second variant. Images and audio are optional; up to 9 images and 3 audio tracks; duration 1-15s; 480p/768p in vertical and horizontal; no 1080p.',
      );
    case 'autodl:minimax-h3-lipsync':
      return t(
        'Single-image audio-synchronized video with automatic lip sync. Requires exactly one image and one audio track; uses audio_duration for 1-15s; supports 480p/768p/1080p and has no prompt field.',
      );
    default:
      return '';
  }
}
