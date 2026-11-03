export default function Loading() {
  return (
    <div className="feed-page" aria-busy="true" aria-live="polite">
      <div className="page-bar">
        <div className="skeleton skeleton-label" style={{ marginBottom: 0, width: 80 }} />
        <div className="skeleton skeleton-meta" style={{ marginBottom: 0, width: 160 }} />
      </div>
      <div className="post-list">
        {[0, 1, 2].map((item) => (
          <div className="skeleton-row" key={item}>
            <div className="skeleton skeleton-index" />
            <div>
              <div className="skeleton skeleton-meta" />
              <div className="skeleton skeleton-line" />
              <div className="skeleton skeleton-line short" />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
