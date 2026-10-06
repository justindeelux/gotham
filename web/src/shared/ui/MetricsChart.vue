<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import {
  areaPath,
  linePath,
  niceCeil,
  splitSegments,
  tickLabel,
} from "./chartModel";
import type { ChartPoint, ChartSeries } from "./chartModel";

export type { ChartPoint, ChartSeries };

const { t } = useI18n();

/**
 * Dependency-free SVG time-series chart.
 *
 * Deliberately hand-rolled instead of pulling in ECharts/Chart.js: the surface
 * is four line charts of one API payload, and a library would add hundreds of
 * kilobytes to the embedded SPA (self-hosted, lean-footprint principle) for
 * features this component does not use.
 *
 * Missing buckets are real gaps in the backend payload (`ListServerMetrics`
 * omits empty buckets), so a line is broken whenever the time between two
 * points exceeds 1.5 buckets — nothing is interpolated through a gap.
 */

interface Props {
  series: ChartSeries[];
  /** Bucket width in milliseconds; used to detect gaps. */
  stepMs: number;
  /** Formats axis labels and the legend readings. */
  formatValue?: (_value: number) => string;
  /** Fixed y ceiling (percent charts use 100); auto when omitted. */
  yMax?: number;
  height?: number;
}

const props = withDefaults(defineProps<Props>(), {
  formatValue: (value: number) => String(value),
  yMax: undefined,
  height: 150,
});

/** viewBox width; the SVG scales to its container. */
const width = 600;
const padLeft = 46;
const padRight = 12;
const padTop = 10;
const padBottom = 22;

const allPoints = computed<ChartPoint[]>(() =>
  props.series.flatMap((item) => item.points),
);

const hasData = computed<boolean>(() => allPoints.value.length > 0);

const xMin = computed<number>(() => {
  const min = Math.min(...allPoints.value.map((point) => point.at));
  return Number.isFinite(min) ? min : 0;
});

const xMax = computed<number>(() => {
  const max = Math.max(...allPoints.value.map((point) => point.at));
  if (!Number.isFinite(max) || max <= xMin.value) {
    return xMin.value + props.stepMs;
  }
  return max;
});

/** yCeiling is the fixed ceiling when given, else a rounded data maximum. */
const yCeiling = computed<number>(() => {
  if (props.yMax !== undefined) {
    return props.yMax;
  }
  const max = Math.max(...allPoints.value.map((point) => point.value), 0);
  return niceCeil(max);
});

const plotWidth = width - padLeft - padRight;
const plotHeight = computed<number>(() => props.height - padTop - padBottom);

/** x maps a timestamp onto the plot. */
function x(at: number): number {
  const span = xMax.value - xMin.value;
  if (span <= 0) {
    return padLeft;
  }
  return padLeft + ((at - xMin.value) / span) * plotWidth;
}

/** y maps a value onto the plot. */
function y(value: number): number {
  const ceiling = yCeiling.value || 1;
  const clamped = Math.min(Math.max(value, 0), ceiling);
  return padTop + plotHeight.value - (clamped / ceiling) * plotHeight.value;
}

/** project maps a point onto the plot for the path builders. */
function project(at: number, value: number): { x: number; y: number } {
  return { x: x(at), y: y(value) };
}

/** baseline is the fill bottom for the single-series area. */
const baseline = computed<number>(() => padTop + plotHeight.value);

/** yTicks are the three gridline labels (0, half, ceiling). */
const yTicks = computed<number[]>(() => [
  0,
  yCeiling.value / 2,
  yCeiling.value,
]);

const xLabels = computed<Array<{ at: number; label: string }>>(() => {
  if (!hasData.value) {
    return [];
  }
  return [
    { at: xMin.value, label: tickLabel(xMin.value, props.stepMs) },
    { at: xMax.value, label: tickLabel(xMax.value, props.stepMs) },
  ];
});

/** latest renders the newest reading per series for the legend. */
function latest(series: ChartSeries): string {
  const point = series.points[series.points.length - 1];
  return point ? props.formatValue(point.value) : "—";
}

/** ariaLabel summarises the chart for screen readers. Series names stay
 * raw technical values; only the sentence renders in the active locale. */
const ariaLabel = computed<string>(() => {
  const names = props.series.map((item) => item.name).join(", ");
  return t("common.chart.seriesOf", { names });
});
</script>

<template>
  <div class="chart">
    <div v-if="series.length > 1" class="chart__legend">
      <span
        v-for="item in series"
        :key="item.name"
        class="chart__legend-item"
      >
        <span class="chart__swatch" :style="{ background: item.color }" />
        {{ item.name }}
        <span class="num chart__latest">{{ latest(item) }}</span>
      </span>
    </div>
    <svg
      v-if="hasData"
      class="chart__svg"
      :viewBox="`0 0 ${width} ${height}`"
      role="img"
      :aria-label="ariaLabel"
    >
      <g class="chart__grid">
        <line
          v-for="tick in yTicks"
          :key="`grid-${tick}`"
          :x1="padLeft"
          :x2="width - padRight"
          :y1="y(tick)"
          :y2="y(tick)"
        />
      </g>
      <g class="chart__ylabels">
        <text
          v-for="tick in yTicks"
          :key="`y-${tick}`"
          :x="padLeft - 6"
          :y="y(tick) + 3"
          text-anchor="end"
        >
          {{ formatValue(tick) }}
        </text>
      </g>
      <g v-for="item in series" :key="item.name" :data-series="item.name">
        <template v-for="group in splitSegments(item.points, stepMs)" :key="group[0].at">
          <polygon
            v-if="series.length === 1 && group.length > 1"
            class="chart__area"
            :points="areaPath(group, project, baseline)"
            :fill="item.color"
          />
          <polyline
            v-if="group.length > 1"
            class="chart__line"
            :points="linePath(group, project)"
            :stroke="item.color"
          />
          <circle
            v-else-if="group.length === 1"
            :cx="x(group[0].at)"
            :cy="y(group[0].value)"
            r="2"
            :fill="item.color"
          />
        </template>
      </g>
      <g class="chart__xlabels">
        <text
          v-for="label in xLabels"
          :key="label.at"
          :x="x(label.at)"
          :y="height - 6"
          :text-anchor="label.at === xMin ? 'start' : 'end'"
        >
          {{ label.label }}
        </text>
      </g>
    </svg>
  </div>
</template>

<style scoped>
.chart {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.chart__legend {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
  font-size: var(--text-xs);
  color: var(--muted);
}

.chart__legend-item {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}

.chart__swatch {
  width: 8px;
  height: 8px;
  border-radius: var(--radius-pill);
}

.chart__latest {
  color: var(--fg-2);
}

.chart__svg {
  display: block;
  width: 100%;
  height: auto;
  overflow: visible;
}

.chart__grid line {
  stroke: var(--border);
  stroke-width: 1;
  vector-effect: non-scaling-stroke;
}

.chart__ylabels text,
.chart__xlabels text {
  fill: var(--muted);
  font-family: var(--font-mono);
  /* The SVG scales with its container, so the axis labels use the smallest
     step of the shared type scale rather than a literal pixel size. */
  font-size: var(--text-xs);
}

.chart__line {
  fill: none;
  stroke-width: 1.6;
  stroke-linejoin: round;
  stroke-linecap: round;
  vector-effect: non-scaling-stroke;
}

.chart__area {
  opacity: 0.12;
  stroke: none;
}
</style>
