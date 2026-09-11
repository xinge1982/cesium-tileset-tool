import * as Cesium from 'cesium'

export interface ZClipConfig {
  enabled?: boolean
  offset?: number
}

export interface TilesetConfig {
  value: string
  label: string
  url?: string
  options?: string[][]
  zClip?: ZClipConfig
}

export interface ManagedTileset {
  key: string
  name: string
  url: string
  visible: boolean
  loading: boolean
  error?: string
  allowZClip: boolean
  zClipEnabled: boolean
  zClipOffset: number
  zClipValue: number
  zClipMin: number
  zClipMax: number
}

export interface HighlightFeatureRequest {
  tilesetKey: string
  propertyName: string
  propertyValue: string | number
  color?: Cesium.Color
  position?: Cesium.Cartesian3
}

export interface LODDebugInfo {
  layerKey: string
  layerName: string
  lod: number
  fileName: string
  uri: string
}

export type LODDebugListener = (visibleContents: LODDebugInfo[]) => void

const HIGHLIGHT_BILLBOARD_IMAGE = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(`
  <svg xmlns="http://www.w3.org/2000/svg" width="40" height="52" viewBox="0 0 40 52">
    <path d="M20 1C9.5 1 1 9.5 1 20c0 14.5 19 31 19 31s19-16.5 19-31C39 9.5 30.5 1 20 1z" fill="#ff3b30" stroke="#fff" stroke-width="2"/>
    <circle cx="20" cy="20" r="7" fill="#fff"/>
  </svg>
`)}`

type FeatureLike = {
  color: Cesium.Color
  getProperty(name: string): unknown
}

type TileContentLike = {
  url?: string
  uri?: string
  _url?: string
  _resource?: { url?: string }
  model?: object
  _model?: object
  featuresLength?: number
  getFeature?: (index: number) => FeatureLike
  innerContents?: unknown[]
  _contents?: unknown[]
}

type ManagedEntry = {
  state: ManagedTileset
  tileset?: Cesium.Cesium3DTileset
  clippingPlane?: Cesium.ClippingPlane
  features: Set<FeatureLike>
}

export class TilesetManager {
  private readonly entries = new Map<string, ManagedEntry>()
  private readonly originalColors = new WeakMap<FeatureLike, Cesium.Color>()
  private readonly highlightedFeatures = new Set<FeatureLike>()
  private highlightRequest: HighlightFeatureRequest | null = null
  private highlightBillboard: Cesium.Entity | null = null
  private lodDebugEnabled = false
  private lodDebugListener: LODDebugListener | null = null
  private visibleLODFrame = -1
  private readonly visibleLODContents = new Map<string, LODDebugInfo>()
  private readonly lodInfoByObject = new WeakMap<object, LODDebugInfo>()

  public constructor(private readonly viewer: Cesium.Viewer) {}

  public async loadAll(configs: TilesetConfig[]): Promise<ManagedTileset[]> {
    this.destroy()

    const entries = configs
      .filter(config => Boolean(config.value && config.url))
      .map(config => {
        const state: ManagedTileset = {
          key: config.value,
          name: config.label || config.value,
          url: config.url!,
          visible: true,
          loading: true,
          allowZClip: Boolean(config.zClip?.enabled),
          zClipEnabled: Boolean(config.zClip?.enabled),
          zClipOffset: Number.isFinite(Number(config.zClip?.offset))
            ? Number(config.zClip?.offset)
            : 0,
          zClipValue: 0,
          zClipMin: -10,
          zClipMax: 50,
        }
        const entry: ManagedEntry = { state, features: new Set() }
        this.entries.set(state.key, entry)
        return { config, entry }
      })

    await Promise.all(entries.map(({ config, entry }) => this.load(config, entry)))
    return this.getLayers()
  }

  public getLayers(): ManagedTileset[] {
    return Array.from(this.entries.values(), entry => entry.state)
  }

  public setLODDebugEnabled(enabled: boolean, listener?: LODDebugListener): void {
    this.lodDebugEnabled = enabled
    if (listener) this.lodDebugListener = listener
    if (!enabled) this.visibleLODContents.clear()
    this.emitVisibleLODContents()
    this.viewer.scene.requestRender()
  }

  public getPickedLODDebugInfo(picked: unknown): LODDebugInfo | undefined {
    if (!this.lodDebugEnabled || !picked || typeof picked !== 'object') return undefined

    const pickedObject = picked as { content?: unknown; primitive?: unknown }
    const primitive = pickedObject.primitive as { content?: unknown } | undefined
    const candidates = [picked, pickedObject.content, primitive, primitive?.content]
    for (const candidate of candidates) {
      if (candidate && typeof candidate === 'object') {
        const info = this.lodInfoByObject.get(candidate)
        if (info) return info
      }
    }
    return undefined
  }

  public setVisible(tilesetKey: string, visible: boolean): void {
    const entry = this.entries.get(tilesetKey)
    if (!entry) return

    entry.state.visible = visible
    if (entry.tileset && !entry.tileset.isDestroyed()) {
      entry.tileset.show = visible
    }
    if (!visible) {
      for (const [key, info] of this.visibleLODContents) {
        if (info.layerKey === tilesetKey) this.visibleLODContents.delete(key)
      }
      this.emitVisibleLODContents()
    }
    this.viewer.scene.requestRender()
  }

  public setZClip(tilesetKey: string, value: number): void {
    const entry = this.entries.get(tilesetKey)
    if (!entry || !entry.state.allowZClip || !Number.isFinite(value)) return

    entry.state.zClipValue = value
    entry.state.zClipMin = Math.min(entry.state.zClipMin, Math.floor(value - 10))
    entry.state.zClipMax = Math.max(entry.state.zClipMax, Math.ceil(value + 10))
    if (entry.clippingPlane) {
      entry.clippingPlane.distance = value
    }
    this.viewer.scene.requestRender()
  }

  public setZClipEnabled(tilesetKey: string, enabled: boolean): void {
    const entry = this.entries.get(tilesetKey)
    if (!entry || !entry.state.allowZClip) return

    entry.state.zClipEnabled = enabled
    if (entry.tileset?.clippingPlanes) {
      entry.tileset.clippingPlanes.enabled = enabled
    }
    this.viewer.scene.requestRender()
  }

  public setHighlightFeature(request: HighlightFeatureRequest | null): void {
    this.restoreHighlight()
    this.highlightRequest = request == null
      ? null
      : {
          ...request,
          propertyValue: String(request.propertyValue),
          color: Cesium.Color.clone(request.color ?? Cesium.Color.INDIANRED),
        }

    if (this.highlightRequest) {
      if (this.highlightRequest.position) {
        this.showHighlightBillboard(this.highlightRequest.position)
      }
      const entry = this.entries.get(this.highlightRequest.tilesetKey)
      if (entry) {
        entry.features.forEach(feature => this.applyHighlight(entry.state.key, feature))
      }
    }
    this.viewer.scene.requestRender()
  }

  public clearHighlightFeature(): void {
    this.setHighlightFeature(null)
  }

  public destroy(): void {
    this.restoreHighlight()
    this.removeHighlightBillboard()
    this.highlightRequest = null

    for (const entry of this.entries.values()) {
      if (entry.tileset && !entry.tileset.isDestroyed()) {
        this.viewer.scene.primitives.remove(entry.tileset)
      }
      entry.features.clear()
    }
    this.entries.clear()
    this.visibleLODContents.clear()
    this.emitVisibleLODContents()
  }

  private async load(config: TilesetConfig, entry: ManagedEntry): Promise<void> {
    try {
      let tileUrl = entry.state.url
      if (tileUrl.indexOf('http') < 0 && tileUrl.indexOf("https") < 0) {
        tileUrl = import.meta.env.VITE_APP_TILESET_URL + tileUrl
      }
      const tileset = await Cesium.Cesium3DTileset.fromUrl(
        tileUrl,
        this.parseOptions(config.options),
      )
      entry.tileset = tileset
      tileset.show = entry.state.visible
      this.viewer.scene.primitives.add(tileset)

      if (entry.state.allowZClip) {
        this.setupZClipping(entry)
      }

      tileset.tileVisible.addEventListener((tile: unknown) => {
        if (this.lodDebugEnabled) this.trackVisibleLODContents(entry, tile)
        for (const content of this.getFeatureContents(tile)) {
          const featuresLength = content.featuresLength ?? 0
          for (let index = 0; index < featuresLength; index += 1) {
            const feature = content.getFeature?.(index)
            if (!feature) continue
            entry.features.add(feature)
            this.applyHighlight(entry.state.key, feature)
          }
        }
      })
    } catch (error) {
      entry.state.error = error instanceof Error ? error.message : String(error)
      console.error(`load tileset ${entry.state.key} failed:`, error)
    } finally {
      entry.state.loading = false
      this.viewer.scene.requestRender()
    }
  }

  private setupZClipping(entry: ManagedEntry): void {
    const tileset = entry.tileset
    if (!tileset) return

    const plane = new Cesium.ClippingPlane(
      new Cesium.Cartesian3(0, 0, -1),
      entry.state.zClipValue,
    )
    entry.clippingPlane = plane
    tileset.clippingPlanes = new Cesium.ClippingPlaneCollection({
      planes: [plane],
      enabled: entry.state.zClipEnabled,
      edgeColor: Cesium.Color.YELLOW,
      edgeWidth: 1,
    })
  }

  private applyHighlight(tilesetKey: string, feature: FeatureLike): void {
    const request = this.highlightRequest
    if (!request || request.tilesetKey !== tilesetKey) return

    let value: unknown
    try {
      value = feature.getProperty(request.propertyName)
    } catch {
      return
    }
    if (value == null || String(value) !== String(request.propertyValue)) return

    if (!this.originalColors.has(feature)) {
      this.originalColors.set(feature, Cesium.Color.clone(feature.color))
    }
    feature.color = Cesium.Color.clone(request.color ?? Cesium.Color.INDIANRED)
    this.highlightedFeatures.add(feature)
  }

  private restoreHighlight(): void {
    for (const feature of this.highlightedFeatures) {
      const color = this.originalColors.get(feature)
      if (color) feature.color = Cesium.Color.clone(color)
    }
    this.highlightedFeatures.clear()
    this.removeHighlightBillboard()
  }

  private logCartesian3Position(
      position: Cesium.Cartesian3,
      label = 'Position',
  ): {
    longitude: number
    latitude: number
    altitude: number
  } {
    const cartographic =
        Cesium.Cartographic.fromCartesian(position)

    const result = {
      longitude: Cesium.Math.toDegrees(
          cartographic.longitude,
      ),
      latitude: Cesium.Math.toDegrees(
          cartographic.latitude,
      ),
      altitude: cartographic.height,
    }

    console.log(
        `${label}:`,
        `lng=${result.longitude.toFixed(8)},`,
        `lat=${result.latitude.toFixed(8)},`,
        `alt=${result.altitude.toFixed(3)} m`,
    )

    return result
  }

  private showHighlightBillboard(position: Cesium.Cartesian3): void {
    this.removeHighlightBillboard()

    const request = this.highlightRequest
    if (!request) return

    this.logCartesian3Position(position)

    const entity = this.viewer.entities.add({
      position: Cesium.Cartesian3.clone(position),

      billboard: {
        image: HIGHLIGHT_BILLBOARD_IMAGE,
        width: 40,
        height: 52,
        verticalOrigin: Cesium.VerticalOrigin.BOTTOM,
        disableDepthTestDistance: Number.POSITIVE_INFINITY,
      },

      label: {
        text: String(request.propertyValue),
        font: '15px sans-serif',

        fillColor: Cesium.Color.WHITE,
        outlineColor: Cesium.Color.BLACK,
        outlineWidth: 3,
        style: Cesium.LabelStyle.FILL_AND_OUTLINE,

        showBackground: true,
        backgroundColor: Cesium.Color.BLACK.withAlpha(0.72),
        backgroundPadding: new Cesium.Cartesian2(8, 5),

        horizontalOrigin: Cesium.HorizontalOrigin.CENTER,
        verticalOrigin: Cesium.VerticalOrigin.BOTTOM,

        // Billboard 高 52px，文字显示在图标上方。
        pixelOffset: new Cesium.Cartesian2(0, -58),

        disableDepthTestDistance: Number.POSITIVE_INFINITY,
      },
    })

    this.highlightBillboard = entity
    // do not clamp to height
    // if (!this.viewer.scene.clampToHeightSupported) return
    //
    // void this.viewer.scene
    //     .clampToHeightMostDetailed([position])
    //     .then(positions => {
    //       if (
    //           this.highlightRequest !== request ||
    //           this.highlightBillboard !== entity
    //       ) {
    //         return
    //       }
    //
    //       const clampedPosition = positions[0]
    //       if (clampedPosition) {
    //         entity.position =
    //             new Cesium.ConstantPositionProperty(clampedPosition)
    //
    //         this.viewer.scene.requestRender()
    //       }
    //     })
    //     .catch(error =>
    //         console.warn('clamp highlight billboard failed:', error),
    //     )
  }

  private removeHighlightBillboard(): void {
    if (!this.highlightBillboard) return
    this.viewer.entities.remove(this.highlightBillboard)
    this.highlightBillboard = null
  }

  private trackVisibleLODContents(entry: ManagedEntry, tile: unknown): void {
    const frameNumber = Number(
      (this.viewer.scene as unknown as { frameState?: { frameNumber?: number } })
        .frameState?.frameNumber ?? -1,
    )
    if (frameNumber !== this.visibleLODFrame) {
      this.visibleLODFrame = frameNumber
      this.visibleLODContents.clear()
    }

    const tileObject = tile && typeof tile === 'object' ? tile as object : undefined
    const contents = this.getAllTileContents(tile)
    let firstInfo: LODDebugInfo | undefined
    for (const content of contents) {
      const uri = this.getTileContentUri(tile, content)
      const info = this.parseLODInfo(entry, uri)
      if (!info) continue

      firstInfo ??= info
      this.visibleLODContents.set(`${entry.state.key}|${info.uri}`, info)
      this.lodInfoByObject.set(content, info)

      const typedContent = content as TileContentLike
      if (typedContent.model) this.lodInfoByObject.set(typedContent.model, info)
      if (typedContent._model) this.lodInfoByObject.set(typedContent._model, info)
      const featuresLength = typedContent.featuresLength ?? 0
      for (let index = 0; index < featuresLength; index += 1) {
        const feature = typedContent.getFeature?.(index)
        if (feature && typeof feature === 'object') {
          this.lodInfoByObject.set(feature, info)
        }
      }
    }
    if (tileObject && firstInfo) this.lodInfoByObject.set(tileObject, firstInfo)
    this.emitVisibleLODContents()
  }

  private emitVisibleLODContents(): void {
    this.lodDebugListener?.(
      Array.from(this.visibleLODContents.values()).sort((left, right) =>
        left.lod - right.lod || left.uri.localeCompare(right.uri),
      ),
    )
  }

  private parseLODInfo(entry: ManagedEntry, uri: string): LODDebugInfo | undefined {
    if (!uri) return undefined
    let decoded = uri
    try {
      decoded = decodeURIComponent(uri)
    } catch {
      // Keep the original URL if it contains malformed escape sequences.
    }
    const path = decoded.split(/[?#]/, 1)[0]
    const match = path.match(/(?:^|\/)(lod([0-3])\.glb)$/i)
    if (!match) return undefined
    return {
      layerKey: entry.state.key,
      layerName: entry.state.name,
      lod: Number(match[2]),
      fileName: match[1],
      uri,
    }
  }

  private getTileContentUri(tile: unknown, content: object): string {
    const typedContent = content as TileContentLike
    const typedTile = tile as {
      _header?: { content?: { uri?: string; url?: string } }
    }
    const candidates = [
      typedContent.url,
      typedContent.uri,
      typedContent._url,
      typedContent._resource?.url,
      typedTile?._header?.content?.uri,
      typedTile?._header?.content?.url,
    ]
    return candidates.find(value => typeof value === 'string' && value.length > 0) ?? ''
  }

  private getAllTileContents(tile: unknown): object[] {
    const content = (tile as { content?: unknown })?.content
    return this.flattenTileContents(content)
  }

  private flattenTileContents(content: unknown): object[] {
    if (!content || typeof content !== 'object') return []
    const typedContent = content as TileContentLike
    const innerContents = Array.isArray(typedContent.innerContents)
      ? typedContent.innerContents
      : Array.isArray(typedContent._contents)
        ? typedContent._contents
        : []
    return innerContents.length > 0
      ? innerContents.flatMap(inner => this.flattenTileContents(inner))
      : [content]
  }

  private getFeatureContents(tile: unknown): TileContentLike[] {
    const content = (tile as { content?: unknown })?.content
    return this.flattenFeatureContents(content)
  }

  private flattenFeatureContents(content: unknown): TileContentLike[] {
    if (!content || typeof content !== 'object') return []

    const typedContent = content as TileContentLike
    const innerContents = Array.isArray(typedContent.innerContents)
      ? typedContent.innerContents
      : Array.isArray(typedContent._contents)
        ? typedContent._contents
        : []

    if (innerContents.length > 0) {
      return innerContents.flatMap(inner => this.flattenFeatureContents(inner))
    }

    return typeof typedContent.featuresLength === 'number' &&
      typeof typedContent.getFeature === 'function'
      ? [typedContent]
      : []
  }

  private parseOptions(options?: string[][]): Cesium.Cesium3DTileset.ConstructorOptions {
    const result: Record<string, unknown> = {}
    for (const pair of options ?? []) {
      if (!Array.isArray(pair) || pair.length < 2 || !pair[0]) continue
      result[pair[0]] = this.parseOptionValue(pair[1])
    }
    return result as Cesium.Cesium3DTileset.ConstructorOptions
  }

  private parseOptionValue(value: string): string | number | boolean {
    if (value === 'true') return true
    if (value === 'false') return false
    if (value.trim() !== '' && Number.isFinite(Number(value))) return Number(value)
    return value
  }
}
