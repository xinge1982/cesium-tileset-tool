
<template>
  <main class="app-shell">
    <header id="main-toolbar" class="top-toolbar">
      <div class="brand-block">
        <span class="eyebrow">CESIUM DB TOOL</span>
        <h1>三维图层维护工具</h1>
      </div>

      <div class="toolbar-actions">
        <el-cascader
            id="source-selector"
            ref="sourceCascaderRef"
            v-model="currentSourcePath"
            :options="tilesetSourceOptions"
            :props="sourceCascaderProps"
            placeholder="请选择图层和数据源"
            filterable
            clearable
            class="wi-250px search-input"
            @change="handleSourceChange"
        />
        <el-input
            id="search-keyword-input"
            v-model="searchKeyword"
            class="search-input wi-250px"
            placeholder="搜索内容"
            clearable
            @keyup.enter="resetSearch"
        />
        <button
            id="search-button"
            type="button"
            class="primary"
            :disabled="currentSourcePath.length !== 2 || searchLoading"
            @click="resetSearch"
        >
          {{ searchLoading ? '搜索中' : '搜索' }}
        </button>
        <el-popover placement="bottom" :width="380" trigger="click">
          <template #reference>
            <button id="layer-manager-button" type="button">图层 ({{ managedLayers.length }})</button>
          </template>
          <div class="layer-menu">
            <div class="layer-menu-header">
              <span>图层管理</span>
              <el-button
                  type="primary"
                  size="small"
                  plain
                  :loading="layersRefreshing"
                  :disabled="layersRefreshing"
                  @click="refreshAllLayers"
              >
                刷新所有图层
              </el-button>
            </div>
            <span v-if="!managedLayers.length" class="layer-empty">暂无可用图层</span>
            <div
                v-for="layer in managedLayers"
                :key="layer.key"
                class="layer-menu-item"
            >
              <el-checkbox
                  v-model="layer.visible"
                  :disabled="layer.loading || Boolean(layer.error)"
                  @change="setLayerVisible(layer.key, $event)"
              >
                {{ layer.name }}
                <span v-if="layer.loading">（加载中）</span>
                <span v-else-if="layer.error" class="layer-error">（加载失败）</span>
              </el-checkbox>

              <div v-if="layer.allowZClip" class="layer-zclip">
                <el-switch
                    v-model="layer.zClipEnabled"
                    size="small"
                    active-text="Z裁切"
                    @change="setLayerZClipEnabled(layer.key, $event)"
                />
                <el-slider
                    v-model="layer.zClipValue"
                    :min="layer.zClipMin"
                    :max="layer.zClipMax"
                    :step="0.05"
                    @input="setLayerZClip(layer.key, $event)"
                    @wheel.prevent="adjustLayerZClipByWheel(layer, $event)"
                />
                <span class="layer-zclip-value">
                  {{ Number(layer.zClipValue).toFixed(2) }} m
                </span>
              </div>
            </div>
          </div>
        </el-popover>
        <el-dropdown trigger="click" @command="handleExportCommand">
          <button
              id="data-export-button"
              type="button"
              :disabled="exportLoading"
          >
            {{ exportLoading ? '导出中' : '数据导出' }}
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="category-statistics">
                导出分类统计
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <button id="default-view-button" type="button" class="primary" @click="flyToDefaultView">默认视角</button>
        <button id="globe-button" type="button" @click="toggleGlobe">{{ globeVisible ? '隐藏地球' : '显示地球' }}</button>
        <button id="camera-button" type="button" @click="debugCameraPosition">相机参数</button>
        <button id="help-button" type="button" @click="startHelpTour()">帮助</button>
        <span class="scene-status">{{ sceneStatus }}</span>
      </div>
    </header>

    <section class="scene-wrap">
      <div id="cesiumContainer" class="cesium-container"></div>

      <aside id="search-results-panel" v-if="searchResults.length" class="search-panel">
        <b>搜索结果 ({{ searchResults.length }})</b>
        <el-table
            :data="searchResults"
            height="260"
            size="small"
            table-layout="fixed"
            scrollbar-always-on
            :row-class-name="searchRowClassName"
            @row-click="focusSearchResult"
        >
          <el-table-column label="聚焦" width="66" align="center">
            <template #default="{ row }">
              <el-tag
                  v-if="isFocusedRow(row)"
                  type="success"
                  size="small"
                  effect="dark"
              >
                当前
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
              v-for="field in searchResultFields"
              :key="field.name"
              :prop="field.name"
              :label="field.label"
              width="180"
              show-overflow-tooltip
          />
        </el-table>

        <el-pagination
            v-model:current-page="searchPageNumber"
            v-model:page-size="searchPageSize"
            :total="searchTotal"
            @current-change="searchShebei"
            @size-change="searchShebei"
        />
      </aside>


      <div class="mouse-coordinates">
        <span>经度 <b>{{ mouseLongitude }}</b></span>
        <span>纬度 <b>{{ mouseLatitude }}</b></span>
        <span>高程 <b>{{ mouseAltitude }}</b></span>
      </div>
    </section>

    <el-tour
        :key="helpTourKey"
        v-model="helpTourVisible"
        :content-style="helpTourContentStyle"
        @change="handleHelpTourChange"
        @close="completeHelpTour"
        @finish="completeHelpTour"
    >
      <el-tour-step
          target="#main-toolbar"
          title="页面功能导航"
          description="这里集中提供数据源选择、搜索、图层管理和场景控制功能。点击下一步开始一次完整的标志牌查询演示。"
          placement="bottom"
      />
      <el-tour-step
          target="#source-selector"
          title="选择数据源"
          description="已自动选择“设备设施 → 标志牌”。也可以在这里切换其他图层及其数据源。"
          placement="bottom"
      />
      <el-tour-step
          target="#search-keyword-input"
          title="输入搜索内容"
          description="已填入示例关键词“施工”。搜索会匹配当前数据源配置中 search=true 的字段。"
          placement="bottom"
      />
      <el-tour-step
          target="#search-button"
          title="执行搜索"
          description="进入本步骤后会自动执行搜索。日常使用时可以点击搜索按钮，或在输入框中按 Enter。"
          placement="bottom"
      />
      <el-tour-step
          target="#search-results-panel"
          title="查看并聚焦结果"
          description="搜索结果显示在这里，演示会自动聚焦第一条数据。以后点击任意一行即可定位并高亮对应设施，点击“修改”可编辑字段。"
          placement="right"
      />
      <el-tour-step
          target="#layer-manager-button"
          title="图层管理"
          description="查看所有已加载图层，控制显隐、刷新图层，并对允许 Z 裁切的图层调整裁切高度。"
          placement="bottom"
      />
      <el-tour-step
          target="#default-view-button"
          title="恢复默认视角"
          description="相机移动后，可点击这里快速返回系统配置的默认观察位置。"
          placement="bottom"
      />
      <el-tour-step
          target="#globe-button"
          title="地球显隐"
          description="切换地球表面的显示状态，便于观察地下、隧道或经过 Z 裁切的模型。"
          placement="bottom"
      />
      <el-tour-step
          target="#camera-button"
          title="相机参数"
          description="输出当前相机的经纬度、高度、朝向、俯仰角和翻滚角，并复制到剪贴板。"
          placement="bottom"
      />
      <el-tour-step
          target="#help-button"
          title="再次查看帮助"
          description="引导结束后，随时点击“帮助”按钮重新播放本演示。"
          placement="bottom"
      />
    </el-tour>
  </main>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import * as Cesium from 'cesium'
import 'cesium/Build/Cesium/Widgets/widgets.css'
import http from './api/http'
import {isNullOrEmpty} from './hooks/use-common'
import {
  TilesetManager,
  type ManagedTileset,
  type TilesetConfig,
} from './modules/tileset-manager'

const sceneStatus = ref('正在初始化')
const mouseLongitude = ref('—')
const mouseLatitude = ref('—')
const mouseAltitude = ref('—')
const globeVisible = ref(true)
const searchKeyword = ref('')
const searchLoading = ref(false)
const searchResults = ref<any[]>([])
const searchTotal = ref(0)
const searchPageNumber = ref(1)
const searchPageSize = ref(20)
const currentSourcePrimaryKey = ref('')
const currentFeatureKey = ref('')
const currentDetail = ref<any>()
const managedLayers = ref<ManagedTileset[]>([])
const layersRefreshing = ref(false)
const exportLoading = ref(false)
const focusedRowIdentity = ref('')
const helpTourVisible = ref(false)
const helpTourKey = ref(0)
const sourceCascaderRef = ref<{
  togglePopperVisible: (visible?: boolean) => void
}>()
const helpTourContentStyle = {
  color: '#303133',
  backgroundColor: '#ffffff',
  '--el-text-color-primary': '#303133',
  '--el-text-color-regular': '#606266',
  '--el-text-color-secondary': '#606266',
}
const HELP_TOUR_STORAGE_KEY = 'cesium-db-tool-help-tour-v1'
const HELP_TOUR_SEARCH_KEYWORD = '施工'
let helpTourAutoStarted = false
let helpTourSearchPromise: Promise<void> | null = null

function downloadFilename(contentDisposition: string | undefined) {
  if (!contentDisposition) return '分类统计.xlsx'

  const utf8Name = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1]
  if (utf8Name) {
    try {
      return decodeURIComponent(utf8Name)
    } catch {
      // Continue with the regular filename when the server value is malformed.
    }
  }

  return contentDisposition.match(/filename="?([^";]+)"?/i)?.[1] ?? '分类统计.xlsx'
}

async function exportCategoryStatistics() {
  exportLoading.value = true
  try {
    const response = await http.get('/exports/category-statistics', {
      responseType: 'blob',
    })
    const blob = new Blob([response.data], {
      type: String(
          response.headers['content-type'] ||
          'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
      ),
    })
    const objectUrl = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = objectUrl
    link.download = downloadFilename(
        String(response.headers['content-disposition'] ?? ''),
    )
    document.body.appendChild(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(objectUrl)
    ElMessage.success('分类统计导出成功')
  } catch (error) {
    console.error('export category statistics failed:', error)
    ElMessage.error('分类统计导出失败')
  } finally {
    exportLoading.value = false
  }
}

function handleExportCommand(command: string | number | object) {
  if (command === 'category-statistics') {
    void exportCategoryStatistics()
  }
}

interface TilesetSourceOption {
  value: string
  label: string
}

interface TilesetMenuOption {
  value: string
  label: string
  type?: string
  url?: string
  options?: string[][]
  children: TilesetSourceOption[]
  idField?: string
  zClip?: {
    enabled?: boolean
    offset?: number
  }
}

interface EditorOptionsConfig {
  source?: string
  url?: string
  method?: string
  dataPath?: string
  valueField?: string
  labelField?: string
  childrenField?: string
}

interface FieldEditorConfig {
  type?: string
  clearable?: boolean
  filterable?: boolean
  props?: Record<string, any>
  options?: EditorOptionsConfig
  onSelect?: {
    setFields?: Record<string, string>
  }
  onClear?: {
    clearFields?: string[]
  }
}

interface SearchResultField {
  name: string
  label: string
  editable?: boolean
  submit?: boolean
  editor?: FieldEditorConfig
}

const tilesetSourceOptions = ref<TilesetMenuOption[]>([])
const currentSourcePath = ref<string[]>([])
const searchResultFields = ref<SearchResultField[]>([])

const sourceCascaderProps = {
  value: 'value',
  label: 'label',
  children: 'children',
  emitPath: true,
} as const

let viewer: Cesium.Viewer | null = null
let mousePositionHandler: Cesium.ScreenSpaceEventHandler | null = null
let tilesetManager: TilesetManager | null = null

const defaultView = {
  lon: 121.78256103,
  lat: 31.11249934,
  height: 180,
  heading: 339.544,
  pitch: -35,
  roll: 0,
}

function flyToDefaultView() {
  if (!viewer) return
  viewer.camera.flyTo({
    destination: Cesium.Cartesian3.fromDegrees(defaultView.lon, defaultView.lat, defaultView.height),
    orientation: {
      heading: Cesium.Math.toRadians(defaultView.heading),
      pitch: Cesium.Math.toRadians(defaultView.pitch),
      roll: Cesium.Math.toRadians(defaultView.roll),
    },
    duration: 0.8,
  })
}

async function getTilesets() {
  try {
    const res = await http.get('/tilesets')
    tilesetSourceOptions.value = Array.isArray(res.data.data)
      ? res.data.data
      : []

    if (tilesetManager) {
      managedLayers.value = await tilesetManager.loadAll(
        tilesetSourceOptions.value as TilesetConfig[],
      )
    }

    maybeStartHelpTour()
  } catch (error) {
    console.error('load tileset source menu failed:', error)
    tilesetSourceOptions.value = []
  }
}

async function refreshAllLayers() {
  if (!tilesetManager || layersRefreshing.value) return

  const previousLayerState = new Map(
    managedLayers.value.map(layer => [
      layer.key,
      {
        visible: layer.visible,
        zClipEnabled: layer.zClipEnabled,
        zClipValue: layer.zClipValue,
      },
    ]),
  )

  try {
    layersRefreshing.value = true

    // Reload the configuration as well as every Cesium tileset resource.
    const response = await http.get('/tilesets')
    const configs = Array.isArray(response.data.data)
      ? response.data.data
      : []
    tilesetSourceOptions.value = configs

    managedLayers.value = await tilesetManager.loadAll(
      configs as TilesetConfig[],
    )

    // Preserve visibility and Z clipping choices for layers that still exist.
    for (const layer of managedLayers.value) {
      const previous = previousLayerState.get(layer.key)
      if (!previous) continue

      layer.visible = previous.visible
      tilesetManager.setVisible(layer.key, previous.visible)

      if (layer.allowZClip) {
        setLayerZClip(layer.key, previous.zClipValue)
        setLayerZClipEnabled(layer.key, previous.zClipEnabled)
      }
    }

    ElMessage.success('所有图层已刷新')
  } catch (error) {
    console.error('refresh all tilesets failed:', error)
    ElMessage.error('刷新图层失败')
  } finally {
    layersRefreshing.value = false
  }
}

function setLayerVisible(key: string, visible: unknown) {
  tilesetManager?.setVisible(key, Boolean(visible))
}

function setLayerZClip(key: string, value: unknown) {
  const z = Number(value)
  if (!Number.isFinite(z)) return

  // Update the Vue proxy before TilesetManager mutates its underlying raw state.
  // Otherwise Vue sees the later proxy assignment as unchanged and skips repainting.
  const layer = managedLayers.value.find(item => item.key === key)
  if (layer) {
    layer.zClipValue = z
    layer.zClipMin = Math.min(layer.zClipMin, Math.floor(z - 10))
    layer.zClipMax = Math.max(layer.zClipMax, Math.ceil(z + 10))
  }

  tilesetManager?.setZClip(key, z)
}

function adjustLayerZClipByWheel(layer: ManagedTileset, event: WheelEvent) {
  const direction = event.deltaY < 0 ? 1 : -1
  const nextValue = Number(
    Cesium.Math.clamp(
      layer.zClipValue + direction * 0.05,
      layer.zClipMin,
      layer.zClipMax,
    ).toFixed(2),
  )
  setLayerZClip(layer.key, nextValue)
}

function setLayerZClipEnabled(key: string, enabled: unknown) {
  const value = Boolean(enabled)
  const layer = managedLayers.value.find(item => item.key === key)
  if (layer) layer.zClipEnabled = value
  tilesetManager?.setZClipEnabled(key, value)
}

function selectHelpTourSource() {
  const deviceGroup = tilesetSourceOptions.value.find(
    option => option.label === '设备设施' || option.label.includes('设备设施'),
  )
  const signSource = deviceGroup?.children?.find(
    option => option.label === '标志牌' || option.label.includes('标志牌'),
  )

  if (!deviceGroup || !signSource) {
    ElMessage.warning('未找到“设备设施 → 标志牌”数据源，请检查配置')
    return false
  }

  currentSourcePath.value = [deviceGroup.value, signSource.value]
  handleSourceChange()
  return true
}

async function handleHelpTourChange(step: number) {
  if (step === 1) {
    selectHelpTourSource()
    await nextTick()
    sourceCascaderRef.value?.togglePopperVisible(true)
    return
  }

  if (step === 2) {
    sourceCascaderRef.value?.togglePopperVisible(false)
    searchKeyword.value = HELP_TOUR_SEARCH_KEYWORD
    await nextTick()
    return
  }

  if (step === 3) {
    if (!selectHelpTourSource()) return
    searchKeyword.value = HELP_TOUR_SEARCH_KEYWORD
    searchPageNumber.value = 1
    helpTourSearchPromise = searchShebei()
    await helpTourSearchPromise
    if (!searchResults.value.length) {
      ElMessage.warning('示例关键词没有搜索到数据，可以修改关键词后再次搜索')
    }
    return
  }

  if (step === 4) {
    await helpTourSearchPromise
    const firstResult = searchResults.value[0]
    if (firstResult) await focusSearchResult(firstResult)
  }
}

function completeHelpTour() {
  sourceCascaderRef.value?.togglePopperVisible(false)
  helpTourVisible.value = false
  try {
    window.localStorage.setItem(HELP_TOUR_STORAGE_KEY, 'completed')
  } catch {
    // localStorage may be disabled; the help button remains available.
  }
}

function startHelpTour(automatic = false) {
  if (!tilesetSourceOptions.value.length) {
    if (!automatic) ElMessage.warning('数据源尚未加载完成，请稍后重试')
    return
  }

  helpTourSearchPromise = null
  helpTourKey.value += 1
  helpTourVisible.value = true
}

function maybeStartHelpTour() {
  if (helpTourAutoStarted) return
  helpTourAutoStarted = true

  try {
    if (window.localStorage.getItem(HELP_TOUR_STORAGE_KEY) === 'completed') {
      return
    }
  } catch {
    // Continue with the one-time in-memory automatic tour.
  }

  window.setTimeout(() => startHelpTour(true), 600)
}

function handleSourceChange() {
  focusedRowIdentity.value = ''
  searchPageNumber.value = 1
  searchTotal.value = 0
  searchResults.value = []
  searchResultFields.value = []
}

function resetSearch() {
  searchPageNumber.value = 1
  searchShebei()
}

async function searchShebei() {
  const [tilesetKey, sourceId] = currentSourcePath.value
  if (!tilesetKey || !sourceId) {
    searchResults.value = []
    searchResultFields.value = []
    searchTotal.value = 0
    return
  }

  try {
    searchLoading.value = true

    const res = await http.get(
      `/tilesets/${encodeURIComponent(tilesetKey)}/sources/${encodeURIComponent(sourceId)}/search`,
      {
        params: {
          keyword: searchKeyword.value.trim(),
          page: searchPageNumber.value,
          pageSize: searchPageSize.value,
        },
      },
    )

    const result = res.data.data
    searchTotal.value = result?.total ?? 0
    searchResults.value = result?.items ?? []
    searchResultFields.value = result?.fields ?? []
    currentSourcePrimaryKey.value = result?.primaryKey
    currentFeatureKey.value = result?.featureIdField
  } catch (error) {
    console.error('search tileset source failed:', error)
    searchResults.value = []
    searchResultFields.value = []
    searchTotal.value = 0
  } finally {
    searchLoading.value = false
  }
}

function buildRowIdentity(
  tilesetKey: string,
  sourceId: string,
  key: unknown,
) {
  return `${tilesetKey}::${sourceId}::${String(key)}`
}

function isFocusedRow(row: Record<string, any>) {
  const [tilesetKey, sourceId] = currentSourcePath.value
  const primaryKey = currentSourcePrimaryKey.value
  if (!tilesetKey || !sourceId || !primaryKey) return false
  return focusedRowIdentity.value === buildRowIdentity(
    tilesetKey,
    sourceId,
    row[primaryKey],
  )
}

function searchRowClassName({ row }: { row: Record<string, any> }) {
  return isFocusedRow(row) ? 'focused-search-row' : ''
}

async function loadSourceDetail(
  tilesetKey: string,
  sourceId: string,
  key: unknown,
) {
  const response = await http.get(
    `/tilesets/${encodeURIComponent(tilesetKey)}/sources/${encodeURIComponent(sourceId)}/detail`,
    { params: { key } },
  )
  return response.data.data
}

function focusSourceDetail(
  tilesetKey: string,
  sourceId: string,
  key: unknown,
  featureKey: unknown,
  result: any,
) {
  if (!viewer) return

  currentDetail.value = result?.item
  const geom = currentDetail.value?.geom
  const highlightPosition = typeof geom === 'string' && geom.trim()
    ? focusWktPolygon(viewer, geom)
    : undefined

  const tileset = tilesetSourceOptions.value.find(
    item => item.value === tilesetKey,
  )
  focusedRowIdentity.value = buildRowIdentity(
    tilesetKey,
    sourceId,
    key,
  )

  if (tileset?.zClip?.enabled && highlightPosition) {
    const focusedHeight = Cesium.Cartographic.fromCartesian(
      highlightPosition,
    ).height
    const offset = Number(tileset.zClip.offset ?? 0)
    const clipZ = Number((
      focusedHeight + (Number.isFinite(offset) ? offset : 0)
    ).toFixed(3))
    setLayerZClip(tilesetKey, clipZ)
    setLayerZClipEnabled(tilesetKey, true)
  }

  const matchId = result?.featureValue
  if (!tileset || isNullOrEmpty(matchId)) return

  tilesetManager?.setHighlightFeature({
    tilesetKey,
    propertyName: `${featureKey}`,
    propertyValue: matchId,
    position: highlightPosition,
  })
}

async function focusSearchResult(item: any) {
  const [tilesetKey, sourceId] = currentSourcePath.value
  const primaryKey = currentSourcePrimaryKey.value
  const featureKey = currentFeatureKey.value
  if (!tilesetKey || !sourceId || !primaryKey || !viewer) return

  const key = item[primaryKey]
  if (isNullOrEmpty(key)) return
  if (isNullOrEmpty(featureKey)) return

  try {
    const result = await loadSourceDetail(tilesetKey, sourceId, key)
    focusSourceDetail(tilesetKey, sourceId, key,featureKey, result)
  } catch (error) {
    console.error('load tileset source detail failed:', error)
    currentDetail.value = undefined
  }
}

function pickPositionStable(screenPosition: Cesium.Cartesian2) {
  if (!viewer) return undefined
  const scene = viewer.scene
  try {
    const picked = scene.pick(screenPosition)
    if (picked && scene.pickPositionSupported) {
      const position = scene.pickPosition(screenPosition)
      if (Cesium.defined(position)) return position
    }
    const ray = viewer.camera.getPickRay(screenPosition)
    return ray ? scene.globe.pick(ray, scene) : undefined
  } catch {
    return undefined
  }
}

function updateMouseCoordinates(position?: Cesium.Cartesian3) {
  if (!Cesium.defined(position)) {
    mouseLongitude.value = '—'
    mouseLatitude.value = '—'
    mouseAltitude.value = '—'
    return
  }
  const cartographic = Cesium.Cartographic.fromCartesian(position)
  mouseLongitude.value = `${Cesium.Math.toDegrees(cartographic.longitude).toFixed(8)}°`
  mouseLatitude.value = `${Cesium.Math.toDegrees(cartographic.latitude).toFixed(8)}°`
  mouseAltitude.value = `${cartographic.height.toFixed(3)} m`
}

function toggleGlobe() {
  if (!viewer) return
  globeVisible.value = !globeVisible.value
  viewer.scene.globe.show = globeVisible.value
  viewer.scene.requestRender()
}

async function copyTextToClipboard(text: string) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text)
    return
  }

  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.left = '-9999px'
  textarea.setAttribute('readonly', '')
  document.body.appendChild(textarea)
  textarea.select()

  const copied = document.execCommand('copy')
  document.body.removeChild(textarea)
  if (!copied) {
    throw new Error('browser rejected clipboard copy')
  }
}

async function debugCameraPosition() {
  if (!viewer) return

  const camera = viewer.camera
  const cartographic = Cesium.Cartographic.fromCartesian(
    camera.positionWC,
  )

  const lon = Cesium.Math.toDegrees(cartographic.longitude)
  const lat = Cesium.Math.toDegrees(cartographic.latitude)
  const height = cartographic.height
  const heading = Cesium.Math.toDegrees(camera.heading)
  const pitch = Cesium.Math.toDegrees(camera.pitch)
  const roll = Cesium.Math.toDegrees(camera.roll)

  const result = {
    lon: Number(lon.toFixed(8)),
    lat: Number(lat.toFixed(8)),
    height: Number(height.toFixed(3)),
    heading: Number(heading.toFixed(3)),
    pitch: Number(pitch.toFixed(3)),
    roll: Number(roll.toFixed(3)),
  }

  const objectText = `{
  lon: ${result.lon},
  lat: ${result.lat},
  height: ${result.height},
  heading: ${result.heading},
  pitch: ${result.pitch},
  roll: ${result.roll},
}`

  console.log('Camera ViewPoint:', result)
  console.log(objectText)

  const logText = [
    'Camera ViewPoint:',
    JSON.stringify(result, null, 2),
    '',
    objectText,
  ].join('\n')

  try {
    await copyTextToClipboard(logText)
    ElMessage.success('相机参数及日志已复制到剪贴板')
  } catch (error) {
    console.error('copy camera viewpoint failed:', error)
    ElMessage.error('相机参数已输出，但复制到剪贴板失败')
  }

  return result
}

onMounted(() => {
  const imageryProvider = new Cesium.UrlTemplateImageryProvider({
    url: 'https://webst01.is.autonavi.com/appmaptile?lang=zh_cn&size=1&scale=1&style=6&x={x}&y={y}&z={z}',
    maximumLevel: 16,
  })

  viewer = new Cesium.Viewer('cesiumContainer', {
    baseLayer: new Cesium.ImageryLayer(imageryProvider),
    baseLayerPicker: false,
    animation: false,
    timeline: false,
    sceneModePicker: false,
    scene3DOnly: true,
    infoBox: true,
    selectionIndicator: true,
    terrainProvider: new Cesium.EllipsoidTerrainProvider(),
  })

  viewer.scene.globe.depthTestAgainstTerrain = false
  viewer.scene.requestRenderMode = true

  mousePositionHandler = new Cesium.ScreenSpaceEventHandler(viewer.scene.canvas)
  mousePositionHandler.setInputAction((movement: Cesium.ScreenSpaceEventHandler.MotionEvent) => {
    updateMouseCoordinates(pickPositionStable(movement.endPosition))
  }, Cesium.ScreenSpaceEventType.MOUSE_MOVE)

  tilesetManager = new TilesetManager(viewer)
  sceneStatus.value = '场景已初始化'
  flyToDefaultView()

  getTilesets()
})

function focusWktPolygon(
    viewer: Cesium.Viewer,
    wkt: string,
): Cesium.Cartesian3 {
  // Supports:
  // POLYGON((lng lat z,...))
  // POLYGON Z ((lng lat z,...))
  const match = wkt.match(
      /^POLYGON(?:\s+Z)?\s*\(\((.*)\)\)$/i,
  )

  if (!match) {
    throw new Error(`Invalid POLYGON WKT: ${wkt}`)
  }

  const positions = match[1]
      .split(',')
      .map(item => {
        const values = item
            .trim()
            .split(/\s+/)
            .map(Number)

        const [longitude, latitude, height = 0] = values

        if (
            !Number.isFinite(longitude) ||
            !Number.isFinite(latitude) ||
            !Number.isFinite(height)
        ) {
          throw new Error(`Invalid WKT coordinate: ${item}`)
        }

        return {
          longitude,
          latitude,
          height,
        }
      })

  if (positions.length < 3) {
    throw new Error(`POLYGON has too few positions: ${wkt}`)
  }

  const cartesianPositions = positions.map(position =>
      Cesium.Cartesian3.fromDegrees(
          position.longitude,
          position.latitude,
          position.height,
      ),
  )

  // The sphere center now includes the WKT Z coordinate.
  const boundingSphere =
      Cesium.BoundingSphere.fromPoints(cartesianPositions)

  const pitch = Cesium.Math.toRadians(-25)

  const cameraRange = Cesium.Math.clamp(
      boundingSphere.radius * 2,
      15,
      40,
  )

  viewer.camera.flyToBoundingSphere(boundingSphere, {
    duration: 1,
    offset: new Cesium.HeadingPitchRange(
        viewer.camera.heading,
        pitch,
        cameraRange,
    ),
  })

  // Can also be used as the Billboard position.
  return Cesium.Cartesian3.clone(
      boundingSphere.center,
  )
}

onBeforeUnmount(() => {
  mousePositionHandler?.destroy()
  tilesetManager?.destroy()
  tilesetManager = null
  viewer?.destroy()
})
</script>

<style scoped>
/* Tour content is teleported to body, so these selectors must be global. */
:global(.el-tour__content) {
  color: #303133;
  background: #ffffff;
  --el-text-color-primary: #303133;
  --el-text-color-regular: #606266;
  --el-text-color-secondary: #606266;
}

:global(.el-tour__title) {
  color: #303133 !important;
}

:global(.el-tour__description) {
  color: #606266 !important;
}

.layer-menu {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 360px;
  overflow-y: auto;
}

.layer-menu-header {
  position: sticky;
  top: 0;
  z-index: 1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-bottom: 8px;
  color: #303133;
  background: var(--el-bg-color-overlay);
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.layer-menu :deep(.el-checkbox) {
  margin-right: 0;
}

.layer-menu-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 2px 0 6px;
  border-bottom: 1px solid var(--el-border-color-extra-light);
}

.layer-zclip {
  display: grid;
  grid-template-columns: 84px minmax(120px, 1fr) 58px;
  align-items: center;
  gap: 10px;
  padding-left: 22px;
}

.layer-zclip :deep(.el-slider) {
  margin: 0;
}

.layer-zclip-value {
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
  text-align: right;
}

.layer-empty {
  color: #909399;
}

.layer-error {
  color: #f56c6c;
}

.search-panel {
  position: absolute;
  z-index: 30;

  top: 14px;
  left: 14px;

  width: 700px;
  height: 360px;

  padding: 12px;

  color: var(--text);
  background: rgba(7, 16, 22, 0.95);

  border: 1px solid var(--line);
  border-radius: 10px;

  box-shadow: 0 8px 26px rgba(0,0,0,.4);

  overflow: hidden;
}


.search-panel .el-table {
  margin-top: 10px;
  width: 100%;
  height: 260px;
}


.search-panel .el-pagination {
  margin-top: 10px;
}

.search-panel {
  --el-table-bg-color: transparent;
  --el-table-tr-bg-color: transparent;
  --el-table-header-bg-color: rgba(255,255,255,.08);
  --el-table-border-color: rgba(255,255,255,.12);
  --el-table-text-color: #dce7eb;
  --el-table-header-text-color: #9fb4bd;
}


.search-panel :deep(.el-table) {
  background: transparent;
  color: #dce7eb;
}


.search-panel :deep(.el-table th.el-table__cell) {
  background: rgba(255,255,255,.08) !important;
  color: #9fb4bd !important;
}


.search-panel :deep(.el-table tr) {
  background: transparent !important;
}


.search-panel :deep(.el-table td.el-table__cell) {
  background: transparent !important;
  color: #dce7eb !important;
  border-bottom-color: rgba(255,255,255,.1);
}

/* Keep the right operation column opaque above horizontally scrolled cells. */
.search-panel :deep(.el-table th.el-table-fixed-column--right),
.search-panel :deep(.el-table td.el-table-fixed-column--right) {
  background: rgb(12, 24, 31) !important;
  box-shadow: -5px 0 8px rgba(0, 0, 0, .22);
  z-index: 3;
}

.search-panel :deep(.el-table__body tr:hover > td.el-table-fixed-column--right) {
  background: rgb(17, 53, 52) !important;
}

.search-panel :deep(.el-table__body tr.focused-search-row > td.el-table-fixed-column--right) {
  background: rgb(19, 70, 62) !important;
}

/* Reserve space inside the scroll view so the horizontal bar does not cover
   the last visible table row. */
.search-panel :deep(.el-table__body-wrapper .el-scrollbar__view) {
  box-sizing: border-box;
  padding-bottom: 18px;
}

.search-panel :deep(.el-table__body-wrapper .el-scrollbar__bar.is-horizontal) {
  display: block;
  height: 10px;
  bottom: 2px;
  padding: 2px;
  background: rgba(205, 232, 229, .22);
  border: 1px solid rgba(205, 232, 229, .28);
  border-radius: 6px;
  opacity: 1;
}

.search-panel :deep(
  .el-table__body-wrapper
  .el-scrollbar__bar.is-horizontal
  > .el-scrollbar__thumb
) {
  min-width: 36px;
  background-color: rgb(88, 232, 202) !important;
  border-radius: 5px;
  opacity: 1 !important;
}

.search-panel :deep(
  .el-table__body-wrapper
  .el-scrollbar__bar.is-horizontal
  > .el-scrollbar__thumb:hover
) {
  background-color: rgb(132, 247, 223) !important;
}


.search-panel :deep(.el-table__body tr:hover > td.el-table__cell) {
  background: rgba(61,214,180,.12) !important;
}

.search-panel :deep(.el-table__body tr.focused-search-row > td.el-table__cell) {
  background: rgba(61,214,180,.24) !important;
  box-shadow: inset 0 1px rgba(61,214,180,.4),
              inset 0 -1px rgba(61,214,180,.4);
}


/* These rules intentionally follow the row-state styles. */
.search-panel :deep(.el-table__body tr:hover > td.el-table-fixed-column--right) {
  background: rgb(17, 53, 52) !important;
}

.search-panel :deep(.el-table__body tr.focused-search-row > td.el-table-fixed-column--right) {
  background: rgb(19, 70, 62) !important;
}


.search-panel :deep(.el-pagination) {
  margin-top: 12px;

  --el-pagination-bg-color: transparent;
  --el-pagination-button-bg-color: rgba(255,255,255,.08);
  --el-pagination-text-color: #dce7eb;
}


.search-panel :deep(.el-pagination button),
.search-panel :deep(.el-pagination .el-pager li) {
  background: rgba(255,255,255,.08);
  color: #dce7eb;
}


.search-panel :deep(.el-pagination .el-pager li.is-active) {
  background: #3dd6b4;
  color: #061411;
}

.search-input {
  display: contents;
}
</style>
