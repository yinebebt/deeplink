export interface Link {
  type?: string;
  short_id?: string;
  url?: string;
  title?: string;
  description?: string;
  image_url?: string;
  image_width?: number;
  image_height?: number;
  image_alt?: string;
  og_type?: string;
  locale?: string;
  created_at?: string;
  updated_at?: string;
  deleted_at?: string;
  metadata?: Record<string, unknown>;
}

export interface LinkResponse extends Link {
  short_link: string;
  clicks: number;
}

export type LinkDraft = Omit<Link, "short_id" | "created_at" | "updated_at" | "deleted_at">;
