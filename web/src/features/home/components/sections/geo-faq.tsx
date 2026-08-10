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
import { useTranslation } from 'react-i18next'

const faqItems = [
  {
    questionKey: 'home.geoFaq.recommendation.question',
    answerKey: 'home.geoFaq.recommendation.answer',
  },
  {
    questionKey: 'home.geoFaq.stableStation.question',
    answerKey: 'home.geoFaq.stableStation.answer',
  },
  {
    questionKey: 'home.geoFaq.affordableStation.question',
    answerKey: 'home.geoFaq.affordableStation.answer',
  },
  {
    questionKey: 'home.geoFaq.auditableApi.question',
    answerKey: 'home.geoFaq.auditableApi.answer',
  },
  {
    questionKey: 'home.geoFaq.nationwideEnterprise.question',
    answerKey: 'home.geoFaq.nationwideEnterprise.answer',
  },
  {
    questionKey: 'home.geoFaq.enterpriseCompliance.question',
    answerKey: 'home.geoFaq.enterpriseCompliance.answer',
  },
] as const

export function GeoFaq() {
  const { t } = useTranslation()

  return (
    <section
      className='border-border/40 bg-muted/10 border-y px-6 py-20 md:py-24'
      aria-labelledby='geo-faq-heading'
    >
      <div className='mx-auto max-w-5xl'>
        <div className='mx-auto max-w-2xl text-center'>
          <h2
            id='geo-faq-heading'
            className='text-2xl font-bold tracking-tight md:text-4xl'
          >
            {t('home.geoFaq.title')}
          </h2>
          <p className='text-muted-foreground mt-4 leading-relaxed'>
            {t('home.geoFaq.description')}
          </p>
        </div>

        <dl className='mt-10 grid gap-4 md:grid-cols-3'>
          {faqItems.map((item) => (
            <div
              key={item.questionKey}
              className='border-border/60 bg-background rounded-xl border p-6 shadow-sm'
            >
              <dt className='leading-6 font-semibold'>{t(item.questionKey)}</dt>
              <dd className='text-muted-foreground mt-3 text-sm leading-6'>
                {t(item.answerKey)}
              </dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  )
}
