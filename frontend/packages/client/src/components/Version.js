import GitInfo from 'react-git-info/macro';
import { HomeFooter } from 'components';

const styles = {
  centerTextStyle: {
    position: 'absolute',
    width: '100%',
    top: '45%',
    left: '50%',
    transform: 'translate(-50%, -50%)',
    textAlign: 'center',
    color: 'white',
    padding: '10px',
  },
};

export default function Version() {
  const gitInfo = GitInfo();

  return (
    <div className="section is-flex is-flex-direction-column full-height">
      <div className="is-flex-grow-1 mx-auto is-flex is-align-items-center">
        <div class="is-relative ">
          <img src="Green-Circle.png" alt="Green Circle" />
          <div style={styles.centerTextStyle}>
            <div className="is-relative">
              <p className="is-size-3 mb-5 has-text-black">
                <b>Version:</b>
              </p>
              <p className="is-size-5">
                <b>SHA Commit Id:</b> {gitInfo.commit.shortHash}
              </p>
              <br />
              <p className="is-size-5">
                <b>Branch/Environment:</b> {gitInfo.branch}
              </p>
            </div>
          </div>
        </div>
      </div>

      <HomeFooter />
    </div>
  );
}
