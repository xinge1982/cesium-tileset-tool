declare global {
  interface ObjKeys {
    [propName: string]: any
  }
  const GLOBAL_VAR: String
  interface Window {
    Cesium?: any
    CesiumNavigation?: any // 或者更具体的类型
  }
}
export {}
