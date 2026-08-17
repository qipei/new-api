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
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'

import { HeaderLogo } from './header-logo'

type PublicBrandProps = {
  name: string
  logo: string
  loading: boolean
  logoLoaded: boolean
  compact: boolean
  subtitle: string
  customLogo?: React.ReactNode
}

export const TOKEN01_LOGO_RADIUS_CLASS = 'rounded-[10px]'

export function Token01Wordmark(props: { className?: string }) {
  return (
    <span className={props.className}>
      token<span className='text-[#e2a600]'>01</span>.net
    </span>
  )
}

export function PublicBrand(props: PublicBrandProps) {
  let logoContent: React.ReactNode = (
    <HeaderLogo
      src={props.logo}
      loading={props.loading}
      logoLoaded={props.logoLoaded}
      className='size-full rounded-[10px] object-contain'
    />
  )
  if (props.customLogo) {
    logoContent = props.customLogo
  }
  if (props.loading) {
    logoContent = <Skeleton className='size-full rounded-[10px]' />
  }

  let nameContent: React.ReactNode = props.name
  if (props.name === 'token01.net') {
    nameContent = <Token01Wordmark />
  }
  if (props.compact) {
    nameContent = props.subtitle
  }

  return (
    <>
      <div
        className={cn(
          'flex shrink-0 items-center justify-center overflow-hidden transition-all duration-500 group-hover:scale-105',
          TOKEN01_LOGO_RADIUS_CLASS,
          props.compact ? 'size-8' : 'size-10'
        )}
      >
        {logoContent}
      </div>
      <span className='flex min-w-0 flex-col leading-[1.15]'>
        <span className='max-w-[12rem] truncate text-[15px] font-black tracking-tight md:text-[17px]'>
          {props.loading ? <Skeleton className='h-4 w-20' /> : nameContent}
        </span>
        <span
          className={cn(
            'text-muted-foreground overflow-hidden text-[11px] transition-all duration-500',
            props.compact ? 'max-h-0 opacity-0' : 'max-h-4 opacity-100'
          )}
        >
          {props.compact ? null : props.subtitle}
        </span>
      </span>
    </>
  )
}
