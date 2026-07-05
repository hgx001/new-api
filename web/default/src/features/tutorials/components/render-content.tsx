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
import { Link } from '@tanstack/react-router'
import { Fragment, type ElementType, createElement } from 'react'

import { CopyButton } from '@/components/copy-button'

import type { ContentNode, TextNode } from '../content'

function extractTextValue(node: TextNode): string {
  if (typeof node === 'string') {
    return node
  }
  if ('value' in node) {
    return node.value
  }
  return ''
}

function nodeKey(node: ContentNode, index: number): string {
  if (node.type === 'codeBlock') {
    return `code-${node.value.slice(0, 24)}-${index}`
  }
  if (node.type === 'paragraph' && node.children.length > 0) {
    const text = extractTextValue(node.children[0])
    return `p-${text.slice(0, 24)}-${index}`
  }
  if (node.type === 'list') {
    return `list-${node.ordered ? 'ol' : 'ul'}-${index}`
  }
  if (node.type === 'image') {
    return `img-${node.src}-${index}`
  }
  if (node.type === 'hint' && node.children.length > 0) {
    const text = extractTextValue(node.children[0])
    return `hint-${text.slice(0, 24)}-${index}`
  }
  return `node-${node.type}-${index}`
}

function listItemKey(item: TextNode[], index: number): string {
  const text = item.map(extractTextValue).join('').slice(0, 24)
  return `li-${text}-${index}`
}

function textNodeKey(node: TextNode, index: number): string {
  const value = extractTextValue(node)
  const kind = typeof node === 'string' ? 'text' : node.type
  return `${kind}-${value.slice(0, 20)}-${index}`
}

function renderTextNode(node: TextNode, index: number) {
  const key = textNodeKey(node, index)
  if (typeof node === 'string') {
    return <Fragment key={key}>{node}</Fragment>
  }
  if (node.type === 'strong') {
    return <strong key={key}>{node.value}</strong>
  }
  if (node.type === 'code') {
    return (
      <code
        key={key}
        className='rounded bg-[#f0f5f3] px-1 py-0.5 font-mono text-sm text-[#3a8f7a]'
      >
        {node.value}
      </code>
    )
  }
  if (node.type === 'link') {
    const className =
      'font-medium text-[#3a8f7a] hover:text-[#1a4a3f] hover:underline'
    if (node.external) {
      return (
        <a
          key={key}
          href={node.href}
          target='_blank'
          rel='noopener noreferrer'
          className={className}
        >
          {node.text}
        </a>
      )
    }
    return (
      <Link key={key} to={node.href} className={className}>
        {node.text}
      </Link>
    )
  }
  return null
}

export function RenderContent({ nodes }: { nodes: ContentNode[] }) {
  return (
    <>
      {nodes.map((node, idx) => {
        if (node.type === 'paragraph') {
          return (
            <p key={nodeKey(node, idx)} className='my-3 leading-7 text-[#334644]'>
              {node.children.map(renderTextNode)}
            </p>
          )
        }

        if (node.type === 'heading') {
          const tag = `h${node.level}` as ElementType
          return createElement(
            tag,
            {
              key: `${node.id}-${idx}`,
              id: node.id,
              className: `group relative scroll-mt-28 font-bold text-[#1a4a3f] ${
                node.level === 3 ? 'mt-8 mb-3 text-lg' : 'mt-6 mb-2 text-base'
              }`,
            },
            <>
              {node.children.map(renderTextNode)}
              <a
                href={`#${node.id}`}
                className='ml-2 inline-block text-[#c8e8dc] opacity-0 transition-opacity group-hover:opacity-100'
                aria-label='链接到本节'
              >
                #
              </a>
            </>
          )
        }

        if (node.type === 'list') {
          const ListTag = node.ordered ? 'ol' : 'ul'
          return (
            <ListTag
              key={nodeKey(node, idx)}
              className={`my-3 pl-5 leading-7 text-[#334644] ${
                node.ordered ? 'list-decimal' : 'list-disc'
              }`}
            >
              {node.items.map((item, i) => (
                <li key={listItemKey(item, i)}>{item.map(renderTextNode)}</li>
              ))}
            </ListTag>
          )
        }

        if (node.type === 'codeBlock') {
          return (
            <div
              key={nodeKey(node, idx)}
              className='group relative my-4 overflow-hidden rounded-lg border border-[#e3eeea] bg-[#f8fbfa]'
            >
              <div className='flex items-center justify-between border-b border-[#e3eeea] bg-[#f0f7f4] px-3 py-1.5'>
                <span className='text-xs font-medium text-[#5a7a72]'>
                  {node.lang || 'text'}
                </span>
                <CopyButton
                  value={node.value}
                  variant='ghost'
                  size='sm'
                  className='h-7 text-[#3a8f7a] hover:bg-[#e3eeea] hover:text-[#1a4a3f]'
                  iconClassName='size-3.5'
                />
              </div>
              <pre className='overflow-x-auto p-4'>
                <code className='block font-mono text-sm leading-6 text-[#1a4a3f]'>
                  {node.value}
                </code>
              </pre>
            </div>
          )
        }

        if (node.type === 'image') {
          return (
            <div
              key={nodeKey(node, idx)}
              className='my-4 overflow-hidden rounded-lg border border-[#e3eeea] bg-[#f8fbfa]'
            >
              <div className='flex aspect-video items-center justify-center bg-[#f0f7f4] text-sm text-[#8aa89e]'>
                <div className='text-center'>
                  <p className='font-medium text-[#5a7a72]'>
                    图片占位符
                  </p>
                  <p className='mt-1'>{node.alt}</p>
                  <p className='mt-1 text-xs opacity-70'>{node.src}</p>
                </div>
              </div>
            </div>
          )
        }

        if (node.type === 'hint') {
          return (
            <div
              key={nodeKey(node, idx)}
              className='my-4 rounded-lg border-l-4 border-[#3a8f7a] bg-[#f0f7f4] px-4 py-3 text-sm leading-6 text-[#334644]'
            >
              {node.children.map(renderTextNode)}
            </div>
          )
        }

        return null
      })}
    </>
  )
}
