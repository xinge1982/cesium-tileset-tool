//复制文本

export const isNullOrEmpty = (str: string | null | undefined): boolean => {
  return str === null || str === undefined || str.length === 0
}

//根据url下载模板
export const downLoadTempByUrl = (url: string) => {
  //得到主键key
  const link = document.createElement('a')
  link.href = url
  document.body.appendChild(link)
  link.click()
}

//下载模板
export const downLoadTemp = (res: { data: BlobPart; headers: { [x: string]: string } }) => {
  //得到主键key
  const url = window.URL.createObjectURL(new Blob([res.data]))
  const link = document.createElement('a')
  link.href = url
  link.setAttribute('download', decodeURI(res.headers['file-name']))
  document.body.appendChild(link)
  link.click()
}

export const resetData = (from: { [x: string]: any }, formString: string) => {
  const backData = JSON.parse(formString)
  Object.keys(backData).forEach((key) => (from[key] = backData[key]))
}

export const reshowData = (addEditForm: { [x: string]: any }, detailData: { [x: string]: any }) => {
  Object.keys(addEditForm).forEach((fItem) => {
    // eslint-disable-next-line no-prototype-builtins
    if (detailData && ![null, undefined, ''].includes(detailData[fItem])) {
      addEditForm[fItem] = detailData[fItem]
    }
  })
}

export const formatRFC3339 = (rfcTime: string | number | Date) => {
  const date = new Date(rfcTime)
  return date.toLocaleString('en-US', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    timeZoneName: 'short'
  })
}

//minio图片显示拼接
// import { listReq } from '@/api/ossConfig'
//
// //取 minio 激活的 bucket
// let bucketName = ''
// listReq({}).then(({ rows }: any) => {
//   rows.forEach((item) => {
//     if (item.status === '0') {
//       bucketName = item.bucketName
//     }
//   })
// })
// console.log(bucketName)

export const spliceMinioUrl = (imageUrl: any) => {
  return `${import.meta.env.VITE_APP_IMAGE_URL}/${imageUrl}`
}

export function isNumericString(value: string): boolean {
  return value.trim() !== '' && !Number.isNaN(Number(value));
}
