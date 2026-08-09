import {
  nextEditableBucketUid,
  type EditableResolutionBucket,
} from './video-pricing-core'

type ResolutionTemplateBucket = {
  name: string
  sizes: string[]
}

export type ImageResolutionTemplate = {
  labelKey: string
  buckets: ResolutionTemplateBucket[]
}

const VIDU_IMAGE_BUCKETS: ResolutionTemplateBucket[] = [
  {
    name: '1k',
    sizes: [
      '1024x1024',
      '720x1440',
      '1024x768',
      '1920x1088',
      '1536x1024',
      '1920x816',
    ],
  },
  {
    name: '2k',
    sizes: [
      '2048x2048',
      '1088x2160',
      '2736x2048',
      '2560x1440',
      '3072x2048',
      '2560x1104',
    ],
  },
  {
    name: '4k',
    sizes: [
      '2880x2880',
      '1440x2880',
      '3312x2480',
      '3840x2160',
      '3520x2352',
      '3840x1648',
    ],
  },
]

const VIDU_Q3_FAST_BUCKETS: ResolutionTemplateBucket[] = [
  {
    name: '1k',
    sizes: [
      '1024x1024',
      '768x1376',
      '848x1264',
      '896x1200',
      '928x1152',
      '1584x672',
      '512x2064',
      '352x2928',
    ],
  },
  {
    name: '2k',
    sizes: [
      '2048x2048',
      '1536x2752',
      '1696x2528',
      '1792x2400',
      '1856x2304',
      '3168x1344',
      '1024x4128',
      '704x5856',
    ],
  },
  {
    name: '4k',
    sizes: [
      '4096x4096',
      '3072x5504',
      '3392x5056',
      '3584x4800',
      '3712x4608',
      '6336x2688',
      '2048x8256',
      '1408x11712',
    ],
  },
]

const VIDU_Q2_PRO_BUCKETS: ResolutionTemplateBucket[] = [
  {
    name: '1k',
    sizes: [
      '1024x1024',
      '768x1376',
      '848x1264',
      '896x1200',
      '928x1152',
      '1584x672',
    ],
  },
  {
    name: '2k',
    sizes: [
      '2048x2048',
      '1536x2752',
      '1696x2528',
      '1792x2400',
      '1856x2304',
      '3168x1344',
    ],
  },
  {
    name: '4k',
    sizes: [
      '4096x4096',
      '3072x5504',
      '3392x5056',
      '3584x4800',
      '3712x4608',
      '6336x2688',
    ],
  },
]

const VIDU_Q2_FAST_BUCKETS: ResolutionTemplateBucket[] = [
  {
    name: '1k',
    sizes: [
      '1024x1024',
      '768x1376',
      '848x1264',
      '896x1200',
      '928x1152',
      '1584x672',
    ],
  },
]

export const IMAGE_RESOLUTION_TEMPLATES: Record<
  string,
  ImageResolutionTemplate
> = {
  'bailian-kling-v3': {
    labelKey: 'Alibaba Bailian Kling V3 Image',
    buckets: [
      { name: '1k', sizes: [] },
      { name: '2k', sizes: [] },
    ],
  },
  'bailian-kling-v3-omni': {
    labelKey: 'Alibaba Bailian Kling V3 Omni Image',
    buckets: [
      { name: '1k', sizes: [] },
      { name: '2k', sizes: [] },
      { name: '4k', sizes: [] },
    ],
  },
  'bailian-vidu-image': {
    labelKey: 'Alibaba Bailian Vidu Image',
    buckets: VIDU_IMAGE_BUCKETS,
  },
  'bailian-vidu-q3-fast': {
    labelKey: 'Alibaba Bailian Vidu Q3 Fast',
    buckets: VIDU_Q3_FAST_BUCKETS,
  },
  'bailian-vidu-q2-pro': {
    labelKey: 'Alibaba Bailian Vidu Q2 Pro',
    buckets: VIDU_Q2_PRO_BUCKETS,
  },
  'bailian-vidu-q2-fast': {
    labelKey: 'Alibaba Bailian Vidu Q2 Fast',
    buckets: VIDU_Q2_FAST_BUCKETS,
  },
}

export function editableBucketsFromTemplate(
  template: ImageResolutionTemplate
): EditableResolutionBucket[] {
  return template.buckets.map((bucket) => ({
    uid: nextEditableBucketUid(),
    name: bucket.name,
    sizes: bucket.sizes.join(', '),
  }))
}
