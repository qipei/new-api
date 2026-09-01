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
// CUSTOM: 任务日志里的视频结果预览（fork 扩展）。
import { ExternalLink, Video } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { IconBadge } from '@/components/ui/icon-badge'

interface VideoPreviewDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 站内代理地址，避免把上游带签名的直链暴露给前端。 */
  src: string
  taskId: string
}

export function VideoPreviewDialog(props: VideoPreviewDialogProps) {
  const { t } = useTranslation()

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={
        <>
          <IconBadge tone='chart-2' size='sm'>
            <Video />
          </IconBadge>
          {t('Video Preview')}
        </>
      }
      contentClassName='sm:max-w-2xl'
      titleClassName='flex items-center gap-2'
      contentHeight='auto'
      bodyClassName='space-y-3'
    >
      {/* 代理端点同时接受会话与令牌鉴权，控制台里可直接播放。 */}
      <video
        key={props.src}
        src={props.src}
        controls
        autoPlay
        preload='metadata'
        className='bg-muted/40 max-h-[60vh] w-full rounded-lg'
      />
      <div className='text-muted-foreground flex items-center justify-between gap-2 text-xs'>
        <span className='truncate font-mono'>{props.taskId}</span>
        <a
          href={props.src}
          target='_blank'
          rel='noopener noreferrer'
          className='hover:text-foreground inline-flex shrink-0 items-center gap-1 hover:underline'
        >
          <ExternalLink className='size-3' />
          {t('Open in new tab')}
        </a>
      </div>
    </Dialog>
  )
}
