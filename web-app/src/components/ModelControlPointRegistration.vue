<template>
  <el-dialog
      v-model="dialogVisible"
      title="模型控制点配准"
      width="800px"
      :modal="false"
      modal-class="model-registration-overlay"
      :lock-scroll="false"
      :close-on-click-modal="false"
      draggable
      destroy-on-close
      @closed="cleanupRegistration"
  >
    <div v-if="context" class="registration-dialog">
      <el-descriptions :column="4" size="small" border>
        <el-descriptions-item label="数据ID">{{ context.key }}</el-descriptions-item>
        <el-descriptions-item label="中心经度">{{ formatCoordinate(context.center.longitude, 8) }}</el-descriptions-item>
        <el-descriptions-item label="中心纬度">{{ formatCoordinate(context.center.latitude, 8) }}</el-descriptions-item>
        <el-descriptions-item label="中心高程">{{ formatCoordinate(context.center.height, 3) }} m</el-descriptions-item>
      </el-descriptions>

      <div class="registration-actions">
        <input
            ref="modelFileInput"
            type="file"
            accept=".glb,model/gltf-binary"
            hidden
            @change="loadLocalModel"
        />
        <el-button type="primary" :loading="modelLoading" @click="modelFileInput?.click()">
          加载本地大桥 GLB
        </el-button>
        <span class="model-name">{{ loadedModelName || '尚未加载模型' }}</span>
        <el-button @click="addControlPointPair">增加控制点对</el-button>
        <el-button
            type="success"
            :loading="solving"
            :disabled="completePointCount < 3"
            @click="solveRegistration"
        >
          计算纠偏矩阵
        </el-button>
      </div>

      <el-alert
          v-if="pickHint"
          :title="pickHint"
          type="warning"
          :closable="false"
          show-icon
      />

      <el-table :data="pointPairs" size="small" max-height="330" empty-text="请增加至少三对不共线控制点">
        <el-table-column prop="id" label="ID" width="78" />
        <el-table-column label="模型点（经度 / 纬度 / 高程）" min-width="245">
          <template #default="{ row }">
            <span>{{ formatPoint(row.modelPoint) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="目标点（经度 / 纬度 / 高程）" min-width="245">
          <template #default="{ row }">
            <span>{{ formatPoint(row.targetPoint) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="误差" width="92">
          <template #default="{ row }">
            {{ residualFor(row.id) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="238" fixed="right">
          <template #default="{ row }">
            <el-button size="small" :disabled="!loadedModel" @click="beginPick(row.id, 'model')">模型点</el-button>
            <el-button size="small" @click="beginPick(row.id, 'target')">目标点</el-button>
            <el-button size="small" type="danger" plain @click="removeControlPointPair(row.id)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-card v-if="solution" class="solution-card" shadow="never">
        <div class="solution-errors">
          <span>RMS误差：<b>{{ solution.rmsError.toFixed(4) }} m</b></span>
          <span>最大误差：<b>{{ solution.maxError.toFixed(4) }} m</b></span>
        </div>
        <div>平移 XYZ：{{ vectorText(solution.translation) }}</div>
        <div>四元数 XYZW：{{ quaternionText(solution.quaternion) }}</div>
      </el-card>
    </div>

    <template #footer>
      <el-button @click="dialogVisible = false">取消并清理</el-button>
      <el-button
          type="primary"
          :loading="confirming"
          :disabled="!solution || !context"
          @click="confirmRegistration"
      >
        确认并保存
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { ElMessage } from 'element-plus'
import * as Cesium from 'cesium'
import http from '../api/http'

export interface RegistrationGeoPoint {
  longitude: number
  latitude: number
  height: number
}

export interface ModelRegistrationContext {
  tilesetKey: string
  sourceId: string
  key: string | number
  center: RegistrationGeoPoint
}

interface ControlPointPair {
  id: string
  modelPoint?: RegistrationGeoPoint
  targetPoint?: RegistrationGeoPoint
}

interface RegistrationSolution {
  translation: { x: number; y: number; z: number }
  quaternion: { x: number; y: number; z: number; w: number }
  rmsError: number
  maxError: number
  residuals: Array<{
    id: string
    errorX: number
    errorY: number
    errorZ: number
    distance: number
  }>
}

const props = defineProps<{
  modelValue: boolean
  viewer: Cesium.Viewer | null
  context: ModelRegistrationContext | null
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'completed'): void
}>()

const dialogVisible = computed({
  get: () => props.modelValue,
  set: value => emit('update:modelValue', value),
})
const modelFileInput = ref<HTMLInputElement>()
const pointPairs = ref<ControlPointPair[]>([])
const modelLoading = ref(false)
const solving = ref(false)
const confirming = ref(false)
const loadedModelName = ref('')
const pickHint = ref('')
const solution = ref<RegistrationSolution>()
const completePointCount = computed(() => pointPairs.value.filter(
  pair => pair.modelPoint && pair.targetPoint,
).length)

let loadedModel: Cesium.Model | null = null
let baseModelMatrix: Cesium.Matrix4 | null = null
let loadedModelObjectURL = ''
let pickHandler: Cesium.ScreenSpaceEventHandler | null = null
let nextPointNumber = 1
const pairEntities = new Map<string, Cesium.Entity[]>()

function formatCoordinate(value: number, digits: number) {
  return Number.isFinite(value) ? value.toFixed(digits) : '—'
}

function formatPoint(point?: RegistrationGeoPoint) {
  if (!point) return '未选择'
  return `${point.longitude.toFixed(8)} / ${point.latitude.toFixed(8)} / ${point.height.toFixed(3)}`
}

function vectorText(value: { x: number; y: number; z: number }) {
  return `${value.x.toFixed(6)}, ${value.y.toFixed(6)}, ${value.z.toFixed(6)}`
}

function quaternionText(value: { x: number; y: number; z: number; w: number }) {
  return `${value.x.toFixed(9)}, ${value.y.toFixed(9)}, ${value.z.toFixed(9)}, ${value.w.toFixed(9)}`
}

function residualFor(id: string) {
  const residual = solution.value?.residuals.find(item => item.id === id)
  return residual ? `${residual.distance.toFixed(4)} m` : '—'
}

function errorMessage(error: any, fallback: string) {
  return String(error?.response?.data?.error || error?.message || fallback)
}

async function loadLocalModel(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || !props.viewer || !props.context) return

  modelLoading.value = true
  removeLoadedModel()
  try {
    loadedModelObjectURL = URL.createObjectURL(file)
    const center = props.context.center
    const origin = Cesium.Cartesian3.fromDegrees(
      center.longitude,
      center.latitude,
      center.height,
    )
    const enuMatrix = Cesium.Transforms.eastNorthUpToFixedFrame(origin)
    const initialRotation = Cesium.Matrix4.fromRotationTranslation(
      Cesium.Matrix3.fromRotationZ(-Cesium.Math.PI_OVER_TWO),
    )
    const modelMatrix = Cesium.Matrix4.multiply(
      enuMatrix,
      initialRotation,
      new Cesium.Matrix4(),
    )
    const model = await Cesium.Model.fromGltfAsync({
      url: loadedModelObjectURL,
      modelMatrix,
    })
    props.viewer.scene.primitives.add(model)
    loadedModel = model
    baseModelMatrix = Cesium.Matrix4.clone(modelMatrix, new Cesium.Matrix4())
    loadedModelName.value = file.name
    props.viewer.scene.requestRender()
    ElMessage.success('本地模型已按当前大桥中心点放置')
  } catch (error) {
    removeLoadedModel()
    ElMessage.error(errorMessage(error, '本地模型加载失败'))
  } finally {
    modelLoading.value = false
  }
}

function removeLoadedModel() {
  if (loadedModel && props.viewer && !props.viewer.isDestroyed()) {
    props.viewer.scene.primitives.remove(loadedModel)
  }
  loadedModel = null
  baseModelMatrix = null
  loadedModelName.value = ''
  if (loadedModelObjectURL) URL.revokeObjectURL(loadedModelObjectURL)
  loadedModelObjectURL = ''
}

function addControlPointPair() {
  clearSolutionAndPreview()
  const id = `CP${String(nextPointNumber++).padStart(2, '0')}`
  pointPairs.value.push({ id })
}

function removeControlPointPair(id: string) {
  cancelPicking()
  clearSolutionAndPreview()
  removePairEntities(id)
  pointPairs.value = pointPairs.value.filter(pair => pair.id !== id)
}

function beginPick(id: string, kind: 'model' | 'target') {
  const viewer = props.viewer
  if (!viewer) return
  if (kind === 'model' && !loadedModel) {
    ElMessage.warning('请先加载本地大桥模型')
    return
  }

  cancelPicking()
  clearSolutionAndPreview()
  setPairEntitiesVisible(false)
  if (kind === 'target' && loadedModel) loadedModel.show = false
  pickHint.value = kind === 'model'
    ? `请在本地模型上选择 ${id} 的模型点`
    : `请在道路面或目标模型上选择 ${id} 的目标点`
  viewer.scene.canvas.style.cursor = 'crosshair'
  viewer.scene.requestRender()
  pickHandler = new Cesium.ScreenSpaceEventHandler(viewer.scene.canvas)
  pickHandler.setInputAction((movement: Cesium.ScreenSpaceEventHandler.PositionedEvent) => {
    const picked = viewer.scene.pick(movement.position)
    if (kind === 'model' && (!picked || picked.primitive !== loadedModel)) {
      ElMessage.warning('请选择本次加载的大桥模型表面')
      return
    }
    const position = pickScenePosition(viewer, movement.position, picked)
    if (!position) {
      ElMessage.warning('当前位置无法取得三维坐标')
      return
    }
    const pair = pointPairs.value.find(item => item.id === id)
    if (!pair) {
      cancelPicking()
      return
    }
    const point = cartesianToGeoPoint(position)
    if (kind === 'model') pair.modelPoint = point
    else pair.targetPoint = point
    clearSolutionAndPreview()
    refreshPairEntities(pair)
    cancelPicking()
  }, Cesium.ScreenSpaceEventType.LEFT_CLICK)
}

function pickScenePosition(
  viewer: Cesium.Viewer,
  screenPosition: Cesium.Cartesian2,
  picked: unknown,
) {
  try {
    if (picked && viewer.scene.pickPositionSupported) {
      const position = viewer.scene.pickPosition(screenPosition)
      if (Cesium.defined(position) && cartesianIsFinite(position)) return position
    }
    const ray = viewer.camera.getPickRay(screenPosition)
    const position = ray ? viewer.scene.globe.pick(ray, viewer.scene) : undefined
    return Cesium.defined(position) && cartesianIsFinite(position) ? position : undefined
  } catch {
    return undefined
  }
}

function cartesianIsFinite(position: Cesium.Cartesian3) {
  return Number.isFinite(position.x) && Number.isFinite(position.y) && Number.isFinite(position.z)
}

function cartesianToGeoPoint(position: Cesium.Cartesian3): RegistrationGeoPoint {
  const value = Cesium.Cartographic.fromCartesian(position)
  return {
    longitude: Cesium.Math.toDegrees(value.longitude),
    latitude: Cesium.Math.toDegrees(value.latitude),
    height: value.height,
  }
}

function geoPointToCartesian(point: RegistrationGeoPoint) {
  return Cesium.Cartesian3.fromDegrees(point.longitude, point.latitude, point.height)
}

function refreshPairEntities(
  pair: ControlPointPair,
  displayedModelPosition?: Cesium.Cartesian3,
) {
  const viewer = props.viewer
  if (!viewer) return
  removePairEntities(pair.id)
  const entities: Cesium.Entity[] = []
  if (pair.modelPoint) {
    const modelPosition = displayedModelPosition ?? geoPointToCartesian(pair.modelPoint)
    entities.push(viewer.entities.add({
      position: modelPosition,
      point: { pixelSize: 11, color: Cesium.Color.RED, outlineColor: Cesium.Color.WHITE, outlineWidth: 2 },
    }))
  }
  if (pair.targetPoint) {
    entities.push(viewer.entities.add({
      position: geoPointToCartesian(pair.targetPoint),
      point: { pixelSize: 11, color: Cesium.Color.LIME, outlineColor: Cesium.Color.WHITE, outlineWidth: 2 },
    }))
  }
  if (pair.modelPoint && pair.targetPoint) {
    const modelPosition = displayedModelPosition ?? geoPointToCartesian(pair.modelPoint)
    const targetPosition = geoPointToCartesian(pair.targetPoint)
    const midpoint = Cesium.Cartesian3.midpoint(modelPosition, targetPosition, new Cesium.Cartesian3())
    entities.push(viewer.entities.add({
      polyline: {
        positions: [modelPosition, targetPosition],
        width: 3,
        material: Cesium.Color.YELLOW,
        depthFailMaterial: Cesium.Color.YELLOW.withAlpha(0.55),
      },
    }))
    entities.push(viewer.entities.add({
      position: midpoint,
      label: {
        text: pair.id,
        font: '16px sans-serif',
        fillColor: Cesium.Color.WHITE,
        showBackground: true,
        backgroundColor: Cesium.Color.BLACK.withAlpha(0.75),
        pixelOffset: new Cesium.Cartesian2(0, -14),
        disableDepthTestDistance: Number.POSITIVE_INFINITY,
      },
    }))
  }
  pairEntities.set(pair.id, entities)
  viewer.scene.requestRender()
}

function removePairEntities(id: string) {
  const viewer = props.viewer
  if (!viewer) return
  for (const entity of pairEntities.get(id) ?? []) viewer.entities.remove(entity)
  pairEntities.delete(id)
}

function setPairEntitiesVisible(visible: boolean) {
  for (const entities of pairEntities.values()) {
    for (const entity of entities) entity.show = visible
  }
}

function cancelPicking() {
  pickHandler?.destroy()
  pickHandler = null
  pickHint.value = ''
  if (props.viewer && !props.viewer.isDestroyed()) {
    props.viewer.scene.canvas.style.cursor = ''
  }
  if (loadedModel) loadedModel.show = true
  setPairEntitiesVisible(true)
  props.viewer?.scene.requestRender()
}

async function solveRegistration() {
  if (!props.context) return
  const points = pointPairs.value.filter(
    (pair): pair is Required<ControlPointPair> => Boolean(pair.modelPoint && pair.targetPoint),
  )
  if (points.length < 3) {
    ElMessage.warning('至少需要三对完整且不共线的控制点')
    return
  }
  solving.value = true
  try {
    const path = registrationApiPath('solve')
    const response = await http.post(path, {
      center: props.context.center,
      points,
    })
    solution.value = response.data.data as RegistrationSolution
    applySolutionPreview(solution.value)
    ElMessage.success('纠偏矩阵计算完成，请检查误差后确认')
  } catch (error) {
    solution.value = undefined
    ElMessage.error(errorMessage(error, '纠偏矩阵计算失败'))
  } finally {
    solving.value = false
  }
}

async function confirmRegistration() {
  if (!props.context || !solution.value || !loadedModel) return
  confirming.value = true
  try {
    const correction = createYAxisForwardToXAxisForwardCorrection()
    const transform = modelMatrixToTilesetTransform(
      loadedModel.modelMatrix,
      correction,
    )
    const transformValues = Cesium.Matrix4.toArray(transform)
    const transformObject = Object.fromEntries(
      transformValues.map((value, index) => [String(index), value]),
    )
    await http.post(registrationApiPath('confirm'), {
      key: String(props.context.key),
      transform: transformObject,
    })
    ElMessage.success('模型纠偏矩阵已保存到 metadata.transform')
    cleanupRegistration()
    emit('completed')
    dialogVisible.value = false
  } catch (error) {
    ElMessage.error(errorMessage(error, '保存模型纠偏参数失败'))
  } finally {
    confirming.value = false
  }
}

function clearSolutionAndPreview() {
  solution.value = undefined
  if (loadedModel && baseModelMatrix) {
    loadedModel.modelMatrix = Cesium.Matrix4.clone(
      baseModelMatrix,
      new Cesium.Matrix4(),
    )
    for (const pair of pointPairs.value) refreshPairEntities(pair)
    props.viewer?.scene.requestRender()
  }
}

function applySolutionPreview(value: RegistrationSolution) {
  if (!loadedModel || !baseModelMatrix || !props.context) return

  const center = props.context.center
  const origin = Cesium.Cartesian3.fromDegrees(
    center.longitude,
    center.latitude,
    center.height,
  )
  const enuMatrix = Cesium.Transforms.eastNorthUpToFixedFrame(origin)
  const inverseENU = Cesium.Matrix4.inverseTransformation(
    enuMatrix,
    new Cesium.Matrix4(),
  )
  const localBase = Cesium.Matrix4.multiply(
    inverseENU,
    baseModelMatrix,
    new Cesium.Matrix4(),
  )
  const rotation = Cesium.Matrix3.fromQuaternion(new Cesium.Quaternion(
    value.quaternion.x,
    value.quaternion.y,
    value.quaternion.z,
    value.quaternion.w,
  ))
  const correction = Cesium.Matrix4.fromRotationTranslation(
    rotation,
    new Cesium.Cartesian3(
      value.translation.x,
      value.translation.y,
      value.translation.z,
    ),
  )
  const correctedLocal = Cesium.Matrix4.multiply(
    correction,
    localBase,
    new Cesium.Matrix4(),
  )
  const correctedModelMatrix = Cesium.Matrix4.multiply(
    enuMatrix,
    correctedLocal,
    new Cesium.Matrix4(),
  )
  loadedModel.modelMatrix = correctedModelMatrix
  const inverseBase = Cesium.Matrix4.inverseTransformation(
    baseModelMatrix,
    new Cesium.Matrix4(),
  )
  const previewDelta = Cesium.Matrix4.multiply(
    correctedModelMatrix,
    inverseBase,
    new Cesium.Matrix4(),
  )
  for (const pair of pointPairs.value) {
    const displayedModelPosition = pair.modelPoint
      ? Cesium.Matrix4.multiplyByPoint(
        previewDelta,
        geoPointToCartesian(pair.modelPoint),
        new Cesium.Cartesian3(),
      )
      : undefined
    refreshPairEntities(pair, displayedModelPosition)
  }
  props.viewer?.scene.requestRender()
}

function modelMatrixToTilesetTransform(
  modelMatrix: Cesium.Matrix4,
  axisCorrection?: Cesium.Matrix4,
): Cesium.Matrix4 {
  const finalMatrix = new Cesium.Matrix4()

  if (axisCorrection) {
    Cesium.Matrix4.multiply(modelMatrix, axisCorrection, finalMatrix)
  } else {
    Cesium.Matrix4.clone(modelMatrix, finalMatrix)
  }

  return finalMatrix
}

function createYAxisForwardToXAxisForwardCorrection(): Cesium.Matrix4 {
  const axis3 = buildAxisCorrectionMatrix3(
    new Cesium.Cartesian3(0, -1, 0),
    new Cesium.Cartesian3(1, 0, 0),
    new Cesium.Cartesian3(0, 0, 1),
  )

  return matrix3ToMatrix4(axis3)
}

function buildAxisCorrectionMatrix3(
  xAxis: Cesium.Cartesian3,
  yAxis: Cesium.Cartesian3,
  zAxis: Cesium.Cartesian3,
) {
  const result = Cesium.Matrix3.clone(
    Cesium.Matrix3.IDENTITY,
    new Cesium.Matrix3(),
  )
  Cesium.Matrix3.setColumn(result, 0, xAxis, result)
  Cesium.Matrix3.setColumn(result, 1, yAxis, result)
  Cesium.Matrix3.setColumn(result, 2, zAxis, result)
  return result
}

function matrix3ToMatrix4(matrix: Cesium.Matrix3) {
  return Cesium.Matrix4.fromRotationTranslation(matrix)
}

function registrationApiPath(action: 'solve' | 'confirm') {
  const value = props.context!
  return `/tilesets/${encodeURIComponent(value.tilesetKey)}/sources/${encodeURIComponent(value.sourceId)}/model-registration/${action}`
}

function cleanupRegistration() {
  cancelPicking()
  for (const id of Array.from(pairEntities.keys())) removePairEntities(id)
  removeLoadedModel()
  pointPairs.value = []
  solution.value = undefined
  nextPointNumber = 1
  if (modelFileInput.value) modelFileInput.value.value = ''
}

onBeforeUnmount(cleanupRegistration)
</script>

<style scoped>
.registration-dialog {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.registration-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.model-name {
  min-width: 160px;
  max-width: 260px;
  overflow: hidden;
  color: var(--el-text-color-secondary);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.solution-card {
  line-height: 1.8;
}

.solution-errors {
  display: flex;
  gap: 32px;
  margin-bottom: 4px;
}

.solution-errors b {
  color: var(--el-color-primary);
}

:global(.model-registration-overlay),
:global(.model-registration-overlay .el-overlay-dialog) {
  pointer-events: none !important;
}

:global(.model-registration-overlay .el-dialog) {
  pointer-events: auto !important;
}
</style>
