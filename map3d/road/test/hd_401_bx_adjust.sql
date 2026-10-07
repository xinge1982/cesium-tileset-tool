-- 目标：
-- 1. 生成 public.hd_401_bx_adjust
-- 2. 使用 hd_401_bx.ldid = hd_424_dlm.road_id 建立标线与道路面的关系
-- 3. 标线上的每个点都只在“关联道路面集合”内寻找最近且合理的道路面
-- 4. 标线点新高程 = 对应道路面高程 + 偏移量（默认 0.02m）
-- 5. 原始点绝不删除；ST_Segmentize 只负责在原始点之间补辅助点
-- 6. 原始点与新增辅助点都会重新匹配高程

DROP FUNCTION IF EXISTS public.fn_adjust_linestring_z_with_related_dlm(
    geometry, bigint, double precision, double precision, double precision, double precision, double precision, double precision
);
DROP FUNCTION IF EXISTS public.fn_adjust_bx_geom_with_related_dlm(
    geometry, bigint, double precision, double precision, double precision, double precision, double precision, double precision
);
DROP PROCEDURE IF EXISTS public.sp_build_hd_401_bx_adjust(
    double precision, double precision, double precision, double precision, double precision, double precision
);

CREATE OR REPLACE FUNCTION public.fn_adjust_linestring_z_with_related_dlm(
    p_line geometry,
    p_ldid bigint,
    p_offset double precision DEFAULT 0.02,
    p_segmentize_m double precision DEFAULT 1.00,
    p_keep_dist_m double precision DEFAULT 2.00,
    p_keep_z_tol_m double precision DEFAULT 0.01,
    p_match_dist_m double precision DEFAULT 2.00,
    p_max_z_diff_m double precision DEFAULT 1.00
)
RETURNS geometry
LANGUAGE plpgsql
STABLE
AS $$
DECLARE
    v_dense geometry;
    v_result geometry;
BEGIN
    IF p_line IS NULL THEN
        RETURN p_line;
    END IF;

    IF p_segmentize_m IS NULL OR p_segmentize_m <= 0 THEN
        p_segmentize_m := 1.00;
    END IF;
    IF p_match_dist_m IS NULL OR p_match_dist_m <= 0 THEN
        p_match_dist_m := 2.00;
    END IF;
    IF p_max_z_diff_m IS NULL OR p_max_z_diff_m <= 0 THEN
        p_max_z_diff_m := 1.00;
    END IF;

    -- 注意：ST_Segmentize 会保留原始顶点，只在顶点之间补点
    v_dense := ST_Segmentize(p_line::geography, p_segmentize_m)::geometry;

    WITH raw_pts AS (
        SELECT
            (dp).path[1] AS ord,
            (dp).geom AS pt
        FROM ST_DumpPoints(v_dense) AS dp
    ),
    matched AS (
        SELECT
            p.ord,
            ST_SetSRID(
                ST_MakePoint(
                    ST_X(p.pt),
                    ST_Y(p.pt),
                    COALESCE(m.face_z, ST_Z(p.pt), 0) + p_offset
                ),
                ST_SRID(p_line)
            ) AS pt
        FROM raw_pts AS p
        LEFT JOIN LATERAL (
            SELECT
                ST_Z(ST_3DClosestPoint(d.geom, p.pt)) AS face_z
            FROM public.hd_424_dlm AS d
            WHERE
                d.road_id = p_ldid
                AND ST_DWithin(
                    ST_Force2D(d.geom)::geography,
                    ST_Force2D(p.pt)::geography,
                    p_match_dist_m
                )
                AND abs(
                    COALESCE(ST_Z(ST_3DClosestPoint(d.geom, p.pt)), 0)
                    - COALESCE(ST_Z(p.pt), 0)
                ) <= p_max_z_diff_m
            ORDER BY
                ST_Distance(
                    ST_Force2D(d.geom)::geography,
                    ST_Force2D(p.pt)::geography
                ) ASC,
                abs(
                    COALESCE(ST_Z(ST_3DClosestPoint(d.geom, p.pt)), 0)
                    - COALESCE(ST_Z(p.pt), 0)
                ) ASC,
                d.id ASC
            LIMIT 1
        ) AS m ON TRUE
        ORDER BY p.ord
    )
    SELECT ST_MakeLine(pt ORDER BY ord)
    INTO v_result
    FROM matched;

    IF v_result IS NULL THEN
        RETURN p_line;
    END IF;

    RETURN v_result;
END;
$$;

CREATE OR REPLACE FUNCTION public.fn_adjust_bx_geom_with_related_dlm(
    p_geom geometry,
    p_ldid bigint,
    p_offset double precision DEFAULT 0.02,
    p_segmentize_m double precision DEFAULT 1.00,
    p_keep_dist_m double precision DEFAULT 2.00,
    p_keep_z_tol_m double precision DEFAULT 0.01,
    p_match_dist_m double precision DEFAULT 2.00,
    p_max_z_diff_m double precision DEFAULT 1.00
)
RETURNS geometry
LANGUAGE plpgsql
STABLE
AS $$
DECLARE
    v_type text;
    v_result geometry;
BEGIN
    IF p_geom IS NULL THEN
        RETURN p_geom;
    END IF;

    v_type := upper(GeometryType(p_geom));

    IF v_type = 'LINESTRING' THEN
        RETURN public.fn_adjust_linestring_z_with_related_dlm(
            p_geom,
            p_ldid,
            p_offset,
            p_segmentize_m,
            p_keep_dist_m,
            p_keep_z_tol_m,
            p_match_dist_m,
            p_max_z_diff_m
        );
    ELSIF v_type = 'MULTILINESTRING' THEN
        WITH parts AS (
            SELECT
                (d).path[1] AS part_idx,
                public.fn_adjust_linestring_z_with_related_dlm(
                    (d).geom,
                    p_ldid,
                    p_offset,
                    p_segmentize_m,
                    p_keep_dist_m,
                    p_keep_z_tol_m,
                    p_match_dist_m,
                    p_max_z_diff_m
                ) AS geom
            FROM ST_Dump(p_geom) AS d
        )
        SELECT ST_Multi(ST_Collect(geom ORDER BY part_idx))
        INTO v_result
        FROM parts;

        RETURN v_result;
    ELSE
        RETURN p_geom;
    END IF;
END;
$$;

CREATE OR REPLACE PROCEDURE public.sp_build_hd_401_bx_adjust(
    IN p_offset double precision DEFAULT 0.02,
    IN p_segmentize_m double precision DEFAULT 1.00,
    IN p_keep_dist_m double precision DEFAULT 2.00,
    IN p_keep_z_tol_m double precision DEFAULT 0.01,
    IN p_match_dist_m double precision DEFAULT 2.00,
    IN p_max_z_diff_m double precision DEFAULT 1.00
)
LANGUAGE plpgsql
AS $$
BEGIN
    DROP TABLE IF EXISTS public.hd_401_bx_adjust;

    CREATE TABLE public.hd_401_bx_adjust (
        LIKE public.hd_401_bx INCLUDING DEFAULTS INCLUDING CONSTRAINTS INCLUDING INDEXES
    );

    COMMENT ON TABLE public.hd_401_bx_adjust IS 'hd地面标线（基于 ldid=road_id 逐点匹配道路面并重算高程，不删除原始点）';

    INSERT INTO public.hd_401_bx_adjust (
        id,
        geom,
        ldid,
        bxdl,
        bxxx,
        ys,
        bxkd,
        bxbl,
        bxcd,
        direction,
        type_name,
        virtual,
        bz,
        startoffset
    )
    SELECT
        b.id,
        public.fn_adjust_bx_geom_with_related_dlm(
            b.geom,
            b.ldid,
            p_offset,
            p_segmentize_m,
            p_keep_dist_m,
            p_keep_z_tol_m,
            p_match_dist_m,
            p_max_z_diff_m
        ) AS geom,
        b.ldid,
        b.bxdl,
        b.bxxx,
        b.ys,
        b.bxkd,
        b.bxbl,
        b.bxcd,
        b.direction,
        b.type_name,
        b.virtual,
        b.bz,
        b.startoffset
    FROM public.hd_401_bx AS b;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_indexes
        WHERE schemaname = 'public'
          AND tablename = 'hd_401_bx_adjust'
          AND indexname = 'idx_hd_401_bx_adjust_geom'
    ) THEN
        CREATE INDEX idx_hd_401_bx_adjust_geom
            ON public.hd_401_bx_adjust
            USING gist (geom);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'hd_401_bx_adjust_pkey'
    ) THEN
        ALTER TABLE public.hd_401_bx_adjust
            ADD CONSTRAINT hd_401_bx_adjust_pkey PRIMARY KEY (id);
    END IF;
END;
$$;

-- 推荐执行：
-- CALL public.sp_build_hd_401_bx_adjust(0.02, 1.00, 2.00, 0.01, 2.00, 1.00);
--
-- 参数说明：
-- p_offset         : 标线高于道路面高度，默认 0.02m
-- p_segmentize_m   : 临时加密步长，默认 1.00m
-- p_keep_dist_m    : 保留兼容参数，当前版本不使用
-- p_keep_z_tol_m   : 保留兼容参数，当前版本不使用
-- p_match_dist_m   : 点匹配道路面的最大平面距离，默认 2.00m
-- p_max_z_diff_m   : 点与候选面最大允许高差，默认 1.00m
